package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var httpClient = &http.Client{
	Timeout: 20 * time.Second,
}

type RemoteAddResponse struct {
	Status  int    `json:"status"`
	Msg     string `json:"msg"`
	Message string `json:"message"`
	Result  struct {
		ID     string `json:"id"`
		LinkID string `json:"linkid"`
		Link   string `json:"link"`
	} `json:"result"`
	ID     string `json:"id"`
	LinkID string `json:"linkid"`
	Link   string `json:"link"`
}

type RemoteUploadItem struct {
	Status       string `json:"status"`
	LinkID       string `json:"linkid"`
	LinkIDAlt    string `json:"linkId"`
	ErrorMessage string `json:"error_message"`
	BytesLoaded  int64  `json:"bytes_loaded"`
	BytesTotal   int64  `json:"bytes_total"`
}

type RemoteCloneResult struct {
	OK       bool
	RemoteID string
	LinkID   string
	Link     string
	Error    string
}

type RemoteStatusResult struct {
	Status string
	LinkID string
	Error  string
}

// StartRemoteClone initiates a remote upload on Streamtape
func StartRemoteClone(ctx context.Context, baseURL, folderID, videoURL string) (*RemoteCloneResult, error) {
	if videoURL == "" {
		return &RemoteCloneResult{OK: false, Error: "videoURL is required"}, nil
	}
	if folderID == "" {
		return &RemoteCloneResult{OK: false, Error: "UPLOAD_FOLDER_ID is required"}, nil
	}

	endpoint := fmt.Sprintf("%s/remote/add?url=%s&folder=%s",
		strings.TrimRight(baseURL, "/"),
		url.QueryEscape(videoURL),
		url.QueryEscape(folderID),
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return &RemoteCloneResult{OK: false, Error: err.Error()}, nil
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var parsed RemoteAddResponse
	if err := json.Unmarshal(bodyBytes, &parsed); err != nil {
		return &RemoteCloneResult{OK: false, Error: string(bodyBytes)}, nil
	}

	remoteID := parsed.Result.ID
	if remoteID == "" {
		remoteID = parsed.ID
	}

	linkID := parsed.Result.LinkID
	if linkID == "" {
		linkID = parsed.LinkID
	}

	link := parsed.Result.Link
	if link == "" {
		link = parsed.Link
	}

	if remoteID == "" && linkID == "" {
		errMsg := parsed.Msg
		if errMsg == "" {
			errMsg = parsed.Message
		}
		if errMsg == "" {
			errMsg = fmt.Sprintf("No upload ID or link ID returned (HTTP %d)", resp.StatusCode)
		}
		return &RemoteCloneResult{OK: false, Error: errMsg}, nil
	}

	return &RemoteCloneResult{
		OK:       true,
		RemoteID: remoteID,
		LinkID:   linkID,
		Link:     link,
	}, nil
}

// CheckRemoteCloneStatus checks progress of an in-flight remote upload
func CheckRemoteCloneStatus(ctx context.Context, baseURL, remoteID string) (*RemoteStatusResult, error) {
	if remoteID == "" {
		return &RemoteStatusResult{Status: "error", Error: "remoteId is required"}, nil
	}

	endpoint := fmt.Sprintf("%s/remote/status?id=%s",
		strings.TrimRight(baseURL, "/"),
		url.QueryEscape(remoteID),
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return &RemoteStatusResult{Status: "error", Error: err.Error()}, nil
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Streamtape can return { "result": { "REMOTE_ID": { ... } } } or { "REMOTE_ID": { ... } }
	var wrapper map[string]json.RawMessage
	if err := json.Unmarshal(bodyBytes, &wrapper); err != nil {
		return &RemoteStatusResult{Status: "error", Error: "invalid JSON response"}, nil
	}

	var rawItem json.RawMessage
	if resBytes, exists := wrapper["result"]; exists {
		var inner map[string]json.RawMessage
		if err := json.Unmarshal(resBytes, &inner); err == nil {
			rawItem = inner[remoteID]
		}
	} else if itemBytes, exists := wrapper[remoteID]; exists {
		rawItem = itemBytes
	}

	if len(rawItem) == 0 {
		return &RemoteStatusResult{Status: "unknown"}, nil
	}

	var item RemoteUploadItem
	if err := json.Unmarshal(rawItem, &item); err != nil {
		return &RemoteStatusResult{Status: "error", Error: "failed to parse upload status object"}, nil
	}

	resolvedLinkID := item.LinkID
	if resolvedLinkID == "" {
		resolvedLinkID = item.LinkIDAlt
	}

	if (item.Status == "finished" || item.Status == "completed") && resolvedLinkID != "" {
		return &RemoteStatusResult{
			Status: "finished",
			LinkID: resolvedLinkID,
		}, nil
	}

	if item.Status == "error" {
		errMsg := item.ErrorMessage
		if errMsg == "" {
			errMsg = "remote upload failed on Streamtape"
		}
		return &RemoteStatusResult{
			Status: "error",
			Error:  errMsg,
		}, nil
	}

	status := item.Status
	if status == "" {
		status = "downloading"
	}

	return &RemoteStatusResult{
		Status: status,
	}, nil
}

// DeleteStreamtapeFile deletes an old video file via the proxy worker
func DeleteStreamtapeFile(ctx context.Context, baseURL, fileID string) bool {
	if fileID == "" {
		return false
	}

	endpoint := fmt.Sprintf("%s/fs/files/delete/%s",
		strings.TrimRight(baseURL, "/"),
		url.PathEscape(fileID),
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		slog.Warn("Failed to build delete request", "fileId", fileID, "error", err.Error())
		return false
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		slog.Warn("Failed to execute delete request", "fileId", fileID, "error", err.Error())
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode >= 200 && resp.StatusCode < 300
}
