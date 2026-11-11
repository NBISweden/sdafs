package sdafs

import (
	jsonv2 "encoding/json/v2"
	"fmt"
	"io"
)

type ServerOrganization struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type ServerType struct {
	Group    string `json:"group"`
	Artifact string `json:"artifact"`
	Version  string `json:"version"`
}

type ServerInfo struct {
	ID           string             `json:"id"`
	Name         string             `json:"name"`
	Description  string             `json:"description"`
	Version      string             `json:"version"`
	Environment  string             `json:"environment"`
	Organization ServerOrganization `json:"organization"`
	Type         ServerType         `json:"type"`
}

// verifyServer checks that the
func (s *SDAfs) verifyServer() (*ServerInfo, error) {

	resp, err := s.doRequest("/service-info", "GET")

	if err != nil {
		return nil, fmt.Errorf("request to service info failed: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("unexpected status (%d) while fetching service info",
			resp.StatusCode)
	}

	servicetext, err := io.ReadAll(resp.Body)

	si := ServerInfo{}
	err = jsonv2.Unmarshal(servicetext, &si)

	if err != nil {
		return nil, fmt.Errorf("unmarshal of service-info failed: %w", err)
	}

	return &si, nil
}
