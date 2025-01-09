package sdafs

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/NBISweden/sdafs/internal/httpreader"

	"github.com/jacobsa/fuse"
	"github.com/jacobsa/fuse/fuseops"
	"github.com/jacobsa/fuse/fuseutil"
	"github.com/neicnordic/crypt4gh/keys"
	"github.com/neicnordic/crypt4gh/streaming"
	"gopkg.in/ini.v1"
)

// SDAfs is the main structure to keep track of our SDA connection
type SDAfs struct {
	fuseutil.NotImplementedFileSystem
	conf           *Conf
	datasets       []string
	loading        []string
	publicC4GHkey  string
	privateC4GHkey [32]byte
	token          string
	Owner          uint32
	Group          uint32
	runAs          uint32
	DirPerms       os.FileMode
	FilePerms      os.FileMode
	inodes         map[fuseops.InodeID]*inode

	startTime time.Time
	client    *http.Client
	maplock   sync.Mutex
	nextInode fuseops.InodeID
	handles   map[fuseops.HandleID]io.ReadSeekCloser
	keyHeader http.Header
}

// inode is the struct to manage a directory entry
type inode struct {
	attr fuseops.InodeAttributes

	dir     bool
	loaded  bool
	key     string
	dataset string
	// For directories, children.
	entries []fuseutil.Dirent
	id      fuseops.InodeID

	fileSize    uint64
	rawFileSize uint64
	totalSize   uint64
}

// addInode adds the passed inode with a new id
func (s *SDAfs) addInode(n *inode) fuseops.InodeID {
	s.checkMaps()

	s.maplock.Lock()
	i := s.nextInode
	n.id = i
	s.inodes[i] = n
	s.nextInode++
	s.maplock.Unlock()

	return i
}

// checkMaps makes sure we don't try to use maps that aren't there
func (s *SDAfs) checkMaps() {
	if s.inodes == nil {
		s.inodes = make(map[fuseops.InodeID]*inode)
	}

	if s.nextInode == 0 {
		s.nextInode = fuseops.RootInodeID
	}

	if s.handles == nil {
		s.handles = make(map[fuseops.HandleID]io.ReadSeekCloser)
	}
}

func (s *SDAfs) GetFileSystemServer() fuse.Server {
	return fuseutil.NewFileSystemServer(s)
}

// readToken extracts the token from the credentials file
func (s *SDAfs) readToken() error {
	if s.conf == nil {
		return fmt.Errorf("No configuration provided")
	}

	f, err := ini.Load(s.conf.CredentialsFile)

	if err != nil {
		return fmt.Errorf("Error while opening credentials file %s: %v",
			s.conf.CredentialsFile,
			err)
	}

	for _, section := range f.Sections() {
		if section.HasKey("access_token") {
			s.token = section.Key("access_token").String()
			return nil
		}
	}

	return fmt.Errorf("No access token found in %s", s.conf.CredentialsFile)
}

func (s *SDAfs) doRequest(relPath, method string) (*http.Response, error) {

	reqURL, err := url.JoinPath(s.conf.RootURL, relPath)
	if err != nil {
		return nil, fmt.Errorf(
			"Couldn't make full URL from root %s relative %s: %v",
			s.conf.RootURL, relPath, err)
	}

	req, err := http.NewRequest(method, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf(
			"Couldn't make request for %s: %v",
			reqURL, err)
	}

	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", s.token))
	return s.client.Do(req)
}

func (s *SDAfs) getDatasets() error {
	r, err := s.doRequest("/metadata/datasets", "GET")

	if err != nil {
		return fmt.Errorf(
			"Error while making dataset request: %v",
			err)

	}

	if r.StatusCode != http.StatusOK {
		return fmt.Errorf(
			"Dataset request didn't return 200, we got %d",
			r.StatusCode)
	}

	text, err := io.ReadAll(r.Body)
	if err != nil {
		return fmt.Errorf(
			"Error while reading dataset response: %v",
			err)
	}

	err = json.Unmarshal(text, &s.datasets)
	if err != nil {
		return fmt.Errorf(
			"Error while doing unmarshal of dataset list %v: %v",
			text, err)
	}

	return nil
}

type Conf struct {
	CredentialsFile  string
	RootURL          string
	RemoveSuffix     bool
	SpecifyUID       bool
	SpecifyGID       bool
	UID              uint32
	GID              uint32
	SpecifyDirPerms  bool
	SpecifyFilePerms bool
	SkipLevels       int
	DirPerms         os.FileMode
	FilePerms        os.FileMode
	HTTPClient       *http.Client
}

type datasetFile struct {
	FileID                    string `json:"fileId"`
	FilePath                  string `json:"filePath"`
	DecryptedFileSize         uint64 `json:"decryptedFileSize"`
	DecryptedFileChecksum     string `json:"decryptedFileChecksum"`
	DecryptedFileChecksumType string `json:"decryptedFileChecksumType"`
	FileStatus                string `json:"fileStatus"`
	FileSize                  uint64 `json:"fileSize"`
	CreatedAt                 string `json:"createdAt"`
	LastModified              string `json:"lastModified"`
	strippedName              string
}

func NewSDAfs(conf *Conf) (*SDAfs, error) {
	n := new(SDAfs)
	n.conf = conf

	err := n.setup()
	if err != nil {
		return nil, fmt.Errorf("setup failed: %v", err)
	}

	err = n.VerifyCredentials()

	if err != nil {
		return nil, fmt.Errorf("error while verifying credentials: %v", err)
	}

	return n, nil
}

func (s *SDAfs) getDatasetContents(datasetName string) ([]datasetFile, error) {
	rel := fmt.Sprintf("/metadata/datasets/%s/files", datasetName)
	r, err := s.doRequest(rel, "GET")

	if err != nil {
		return nil, fmt.Errorf(
			"error while making dataset request: %v",
			err)
	}

	if r.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(
			"dataset request didn't return 200, we got %d",
			r.StatusCode)
	}

	var contents []datasetFile

	text, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, fmt.Errorf(
			"error while reading dataset content response: %v",
			err)
	}

	err = json.Unmarshal(text, &contents)
	if err != nil {
		return nil, fmt.Errorf(
			"error while doing unmarshal of dataset contents %v: %v",
			text, err)
	}

	// Until the archive presents the view they want to expose
	for i := range contents {
		fp := contents[i].FilePath
		_, fpnew, found := strings.Cut(fp, "/")

		if found {
			contents[i].FilePath = fpnew
		}
	}

	return contents, nil
}

func (s *SDAfs) createRoot() {

	entries := make([]fuseutil.Dirent, 0)

	dirAttrs := fuseops.InodeAttributes{
		Nlink: 1,
		Mode:  s.DirPerms | os.ModeDir,
		Uid:   s.Owner,
		Gid:   s.Group,
		Mtime: s.startTime,
		Ctime: s.startTime,
		Atime: s.startTime,
	}

	root := inode{dir: true,
		attr: dirAttrs,
	}
	s.addInode(&root)

	for i, dataset := range s.datasets {

		datasetInode := inode{
			attr:    dirAttrs,
			dir:     true,
			dataset: dataset,
			key:     "/",
		}

		in := s.addInode(&datasetInode)

		e := fuseutil.Dirent{
			Offset: fuseops.DirOffset(i + 1), // #nosec G115
			Inode:  in,
			Name:   dataset,
		}

		entries = append(entries, e)
	}

	root.entries = entries
}

// loadDataset brings in the requested dataset
func (s *SDAfs) loadDataset(dataSetName string) error {

	contents, err := s.getDatasetContents(dataSetName)
	if err != nil {
		return fmt.Errorf("error while getting contents for %s: %v",
			dataSetName, err)
	}

	var datasetBase *inode

	// Find the inode to attach stuff to by looking at the root and checking
	// entries
	for _, in := range s.inodes[fuseops.RootInodeID].entries {
		if in.Name == dataSetName {
			datasetBase = s.inodes[in.Inode]

			break
		}
	}

	if datasetBase == nil {
		return fmt.Errorf("didn't find dataset %s in root inode", dataSetName)
	}

	// Go through the list of files and attach, create directory entries and
	// attach as needed
	doneDirs := make(map[string]*inode)
	doneDirs[""] = s.inodes[datasetBase.id]

	s.trimNames(&contents)

	for _, entry := range contents {
		s.attachSDAObject(doneDirs, entry, dataSetName)
	}

	return nil
}

func (s *SDAfs) trimNames(contents *[]datasetFile) {

	for _, entry := range *contents {

		split := strings.Split(entry.FilePath, "/")
		fileName := split[len(split)-1]

		if s.conf.RemoveSuffix {
			//	We should remove c4gh suffix
			stripped := strings.TrimSuffix(fileName, ".c4gh")
			strippedFull := strings.TrimSuffix(entry.FilePath, ".c4gh")

			// Make sure there doesn't already exist the same name as we'd get
			// by stripping

			found := false
			for _, p := range *contents {
				if p.FilePath == strippedFull {
					found = true
					break
				}
			}

			// No problem, present the stripped name
			if !found {
				fileName = stripped
			}
		}

		entry.strippedName = fileName
	}
}

func (s *SDAfs) attachSDAObject(doneDirs map[string]*inode,
	entry datasetFile,
	dataSetName string) {
	if entry.FileStatus != "ready" {
		return
	}

	split := strings.Split(entry.FilePath, "/")

	if s.conf.SkipLevels > 0 {
		split = split[s.conf.SkipLevels:]
	}

	if len(split) < 2 {
		// Entry directly in the base of the dataset
		return
	}

	parent := ""

	// First make sure the directory structure needed for the file exists
	for i := 1; i < len(split); i++ {
		consider := filepath.Join(split[:i]...)

		_, found := doneDirs[consider]

		if found {
			// Already "created" the directory for this entry
			parent = consider
			continue
		}

		// Not found, we need to make a directory entry and attach it
		// to parent

		dirInode := inode{
			attr: fuseops.InodeAttributes{
				Nlink: 1,
				Mode:  s.DirPerms | os.ModeDir,
				Uid:   s.Owner,
				Gid:   s.Group,
				Mtime: s.startTime,
				Ctime: s.startTime,
				Atime: s.startTime,
			},
			dir:     true,
			dataset: dataSetName,
			key:     consider,
			entries: make([]fuseutil.Dirent, 0),
		}

		dIn := s.addInode(&dirInode)
		doneDirs[consider] = &dirInode

		parentInode := s.inodes[doneDirs[parent].id]

		newEntry := fuseutil.Dirent{
			Offset: fuseops.DirOffset(len(parentInode.entries) + 1), // #nosec G115
			Inode:  dIn,
			Name:   split[i-1],
		}

		parentInode.entries = append(parentInode.entries, newEntry)

		parent = consider
	}

	// The directory structure should exist now

	mtime, err := time.Parse(time.RFC3339, entry.LastModified)
	if err != nil {
		log.Printf("Error while decoding modified for %s timestamp %s : %v",
			entry.FileID, entry.LastModified, err)
	}
	ctime, err := time.Parse(time.RFC3339, entry.CreatedAt)
	if err != nil {
		log.Printf("Error while decoding created for %s timestamp %s: %v",
			entry.FileID, entry.CreatedAt, err)
	}

	dirName := filepath.Join(split[:len(split)-1]...)

	fInode := inode{
		attr: fuseops.InodeAttributes{
			Nlink: 1,
			Uid:   s.Owner,
			Gid:   s.Group,
			Mtime: mtime,
			Ctime: ctime,
			Atime: mtime,
			Mode:  s.FilePerms,
			Size:  entry.DecryptedFileSize,
		},
		dir:         false,
		dataset:     dataSetName,
		key:         entry.FilePath,
		fileSize:    entry.DecryptedFileSize,
		rawFileSize: entry.FileSize,
	}
	fIn := s.addInode(&fInode)

	parentInode := s.inodes[doneDirs[dirName].id]

	newEntry := fuseutil.Dirent{
		Offset: fuseops.DirOffset(len(parentInode.entries) + 1), // #nosec G115
		Inode:  fIn,
		Name:   entry.strippedName,
	}

	parentInode.entries = append(parentInode.entries, newEntry)

}

func idToNum(s string) uint32 {
	s = strings.TrimSpace(s)
	n, err := strconv.Atoi(s)

	if err != nil {
		log.Printf("Error while converting %s to number: %v", s, err)
		return 0
	}

	return uint32(n) // #nosec G115
}

// setup makes various initialisations
func (s *SDAfs) setup() error {
	//

	if s.conf == nil {
		return fmt.Errorf("no configuration specified")
	}

	currentUser, err := user.Current()
	s.runAs = idToNum(currentUser.Uid)

	if err == nil {
		s.Owner = idToNum(currentUser.Uid)
		s.Group = idToNum(currentUser.Gid)
	}

	if s.conf.SpecifyUID {
		s.Owner = s.conf.UID
	}

	if s.conf.SpecifyGID {
		s.Group = s.conf.GID
	}

	//
	if s.conf.HTTPClient != nil {
		s.client = s.conf.HTTPClient
	} else {
		transport := http.Transport{IdleConnTimeout: 1800 * time.Second}
		s.client = &http.Client{Transport: &transport}
	}

	s.DirPerms = 0500
	s.FilePerms = 0400

	if s.conf.SpecifyDirPerms {
		s.DirPerms = s.conf.DirPerms
	}

	if s.conf.SpecifyFilePerms {
		s.FilePerms = s.conf.FilePerms
	}

	s.checkMaps()
	s.startTime = time.Now()

	publicKey, privateKey, err := keys.GenerateKeyPair()

	if err != nil {
		return fmt.Errorf("unexpected error while generating c4gh keypair: %v",
			err)
	}
	s.privateC4GHkey = privateKey
	w := new(bytes.Buffer)

	err = keys.WriteCrypt4GHX25519PublicKey(w, publicKey)

	if err != nil {
		return fmt.Errorf("error when writing public key: %v", err)
	}

	publicKeyEncoded := base64.StdEncoding.EncodeToString(w.Bytes())
	s.publicC4GHkey = publicKeyEncoded

	s.keyHeader = make(http.Header)
	s.keyHeader.Add("Client-Public-Key", publicKeyEncoded)

	return nil
}

func (s *SDAfs) VerifyCredentials() error {

	err := s.readToken()
	if err != nil {
		return fmt.Errorf("error while getting token: %v",
			err)
	}

	err = s.getDatasets()
	if err != nil {
		return fmt.Errorf("error while getting datasets: %v",
			err)
	}

	s.createRoot()
	return nil
}

// checkPerms verifies that the operation
func (s *SDAfs) checkPerms(o *fuseops.OpContext) error {
	if s.runAs == o.Uid {
		return nil
	}

	// TODO: Simplified check here is enough?
	if s.FilePerms == os.FileMode(0400) {
		return fuse.EIO
	}

	return nil

	// return fuse.EINVAL?
}

// GetInodeAttributes fills in the required attributes for the given inode
func (s *SDAfs) GetInodeAttributes(
	_ context.Context,
	op *fuseops.GetInodeAttributesOp) error {

	in, ok := s.inodes[op.Inode]
	if !ok {
		return fuse.ENOENT
	}

	op.Attributes = in.attr

	return nil
}

// checkLoaded checks if the dataset the inode is the base for is already loaded
func (s *SDAfs) checkLoaded(i *inode) error {

	if i.loaded {
		return nil
	}

	s.maplock.Lock()

	// Check if we're already loading this and fail if we're doing that.
	if slices.Contains(s.loading, i.dataset) {
		s.maplock.Unlock()
		return fmt.Errorf("already in the process of loading %s", i.dataset)
	}

	s.loading = append(s.loading, i.dataset)
	s.maplock.Unlock()

	log.Printf("Loading dataset %v", i.dataset)

	err := s.loadDataset(i.dataset)
	if err != nil {
		// log.Printf("Couldn't load dataset %s: %v", i.dataset, err)
		return fmt.Errorf("couldn't load dataset %s: %v", i.dataset, err)
	}
	i.loaded = true
	s.maplock.Lock()
	index := slices.Index(s.loading, i.dataset)
	s.loading = slices.Delete(s.loading, index, index+1)
	s.maplock.Unlock()
	return nil
}

func (s *SDAfs) LookUpInode(
	_ context.Context,
	op *fuseops.LookUpInodeOp) error {

	parent, ok := s.inodes[op.Parent]
	if !ok {
		return fuse.ENOENT
	}

	if parent.key == "/" {
		err := s.checkLoaded(parent)
		if err != nil {
			return fuse.EIO
		}
	}

	var child *inode
	found := false

	for _, entry := range parent.entries {
		if entry.Name == op.Name {
			found = true
			child = s.inodes[entry.Inode]
			break
		}
	}

	// Not found in list
	if !found {
		return fuse.ENOENT
	}

	// Copy over information.
	op.Entry.Child = child.id
	op.Entry.Attributes = child.attr

	return nil
}

func (s *SDAfs) StatFS(
	_ context.Context,
	op *fuseops.StatFSOp) error {
	// TODO: Should we fill this in better?

	op.BlockSize = 65536

	return nil
}

// OpenFile provides open(2)
func (s *SDAfs) OpenFile(
	_ context.Context,
	op *fuseops.OpenFileOp) error {

	err := s.checkPerms(&op.OpContext)
	if err != nil {
		return err
	}

	in, found := s.inodes[op.Inode]
	if !found {
		return fuse.EEXIST
	}

	if in.dir {
		return fuse.EINVAL
	}

	r, err := httpreader.NewHTTPReader(s.getFileURL(in), s.token, s.getTotalSize(in), s.client, &s.keyHeader)
	if err != nil {
		log.Printf("openfile failed: reader for %s gave: %v", in.key, err)
		return fuse.EIO
	}

	var inodeReader io.ReadSeekCloser

	c4ghr, err := streaming.NewCrypt4GHReader(r, s.privateC4GHkey, nil)

	if err != nil {
		// Assume we are served non-encrypted content
		inodeReader = r
		log.Printf("Opening of %s as crypt4gh failed: %v", in.key, err)
	} else {
		inodeReader = c4ghr
	}

	// Note: HTTPReader supports Close but doesn't really care for it so
	// we don't go through the trouble of closing it at the end if we're doing
	// crypt4gh

	s.maplock.Lock()
	defer s.maplock.Unlock()

	id, err := s.getNewIDLocked()

	if err != nil {
		return fmt.Errorf("error while getting new ID: %v", err)
	}

	s.handles[id] = inodeReader
	op.Handle = id

	return nil
}

func getRandomID() (uint64, error) {
	b := make([]byte, 8)
	got, err := rand.Read(b)

	if err != nil {
		return 0, fmt.Errorf("error while reading random number: %v", err)
	}

	if got != 8 {
		return 0, fmt.Errorf("got less data when expected - %d instead of 8",
			got)
	}

	var v uint64
	for _, c := range b {
		v = v*256 + uint64(c)
	}

	return v, nil
}

// getNewIdLocked generates an id that isn't previously used
func (s *SDAfs) getNewIDLocked() (fuseops.HandleID, error) {
	for {
		newID, err := getRandomID()
		if err != nil {
			return 0, fmt.Errorf("couldn't make random id: %v", err)
		}

		id := fuseops.HandleID(newID)
		_, exist := s.handles[id]
		if !exist {
			return id, nil
		}
	}
}

// getFileURL returns the path to use when requesting the HTTPReader
func (s *SDAfs) getFileURL(i *inode) string {

	u, err := url.JoinPath(s.conf.RootURL, "/s3-encrypted/", i.dataset, i.key)

	if err != nil {
		log.Printf("Failed to create access URL for %s: %v", i.key, err)
		return ""
	}

	return u
}

// getTotalSize figures out the raw size of the object as presented
func (s *SDAfs) getTotalSize(i *inode) uint64 {

	if i.totalSize != 0 {
		return i.totalSize
	}

	url, err := url.JoinPath("s3-encrypted", i.dataset, i.key)
	if err != nil {
		log.Printf("Error while making URL for size for %s: %v", i.key, err)
		return 0
	}

	r, err := s.doRequest(url, "HEAD")
	if err != nil {
		log.Printf("F URL for %s", i.key)
		return 0
	}

	size := r.Header.Get("Content-Length")
	if size != "" {
		var rawSize uint64
		n, err := fmt.Sscanf(size, "%d", &rawSize)

		if err == nil && n == 1 {
			i.totalSize = rawSize
			return rawSize
		}
	}

	// No content-length or issue decoding, check with server-additional-bytes
	extra := r.Header.Get("Server-Additional-Bytes")
	if extra != "" {
		var extraBytes uint64
		n, err := fmt.Sscanf(extra, "%d", &extraBytes)

		if err == nil && n == 1 {
			i.totalSize = extraBytes + i.rawFileSize

			return extraBytes + i.rawFileSize
		}
	}

	// No header helps, use a reasonable default

	log.Printf("HEAD didn't help for total size for %s, use default 124", url)
	i.totalSize = 124 + i.rawFileSize

	return 124 + i.rawFileSize
}

func (s *SDAfs) ReadFile(
	_ context.Context,
	op *fuseops.ReadFileOp) error {

	r, exist := s.handles[op.Handle]

	if !exist {
		return fuse.EIO
	}

	// r := *reader
	pos, err := r.Seek(op.Offset, io.SeekStart)
	if err != nil || pos != op.Offset {
		return fuse.EIO
	}

	op.BytesRead, err = r.Read(op.Dst)

	if err != nil && err != io.EOF {
		log.Printf("Reading gave error %v", err)
	}

	// Special case: FUSE doesn't expect us to return io.EOF.
	if err == io.EOF {
		return nil
	}

	return err
}
func (s *SDAfs) OpenDir(
	_ context.Context,
	op *fuseops.OpenDirOp) error {

	return s.checkPerms(&op.OpContext)
}

func (s *SDAfs) ReadDir(
	_ context.Context,
	op *fuseops.ReadDirOp) error {

	info, ok := s.inodes[op.Inode]
	if !ok {
		return fuse.ENOENT
	}

	if !info.dir {
		return fuse.EIO
	}

	if info.key == "/" {
		err := s.checkLoaded(info)
		if err != nil {
			return fuse.EIO
		}
	}

	entries := info.entries

	if op.Offset > fuseops.DirOffset(len(entries)) {
		return nil
	}

	entries = entries[op.Offset:]

	for _, entry := range entries {
		n := fuseutil.WriteDirent(op.Dst[op.BytesRead:], entry)
		if n == 0 {
			break
		}

		op.BytesRead += n
	}

	return nil
}
