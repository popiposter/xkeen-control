//go:build linux

package httpapi

import (
	"github.com/popiposter/xkeen-control/internal/auth"
	"github.com/popiposter/xkeen-control/internal/authority"
	"github.com/popiposter/xkeen-control/internal/xkeen"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestNativeJobsRequireSessionCSRFAndPrivateOwner(t *testing.T) {
	dir := t.TempDir()
	passwordPath := filepath.Join(dir, "password.bcrypt")
	if err := setHTTPTestPassword(passwordPath, []byte("synthetic-control-password")); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, "xkeen")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nprintf 'native prompt: '\nread -r answer\n"), 0700); err != nil {
		t.Fatal(err)
	}
	jobs := xkeen.NewJobs(binary, authority.NewLease())
	server := httptest.NewServer(New(Config{Auth: auth.NewManager(auth.Config{HashPath: passwordPath}), NativeJobs: jobs}))
	defer server.Close()
	client := &http.Client{Jar: mustCookieJar(t)}
	response := postJSON(t, client, server.URL+"/api/v1/xkeen/jobs/start", xkeen.CommandRequest{Action: "geodata-schedule"}, "")
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatal(response.StatusCode)
	}
	response.Body.Close()
	login := func(c *http.Client) string {
		response := postJSON(t, c, server.URL+"/api/v1/session/login", map[string]string{"password": "synthetic-control-password"}, "")
		var result struct {
			CSRFToken string `json:"csrfToken"`
		}
		decodeResponse(t, response, &result)
		return result.CSRFToken
	}
	csrf := login(client)
	response = postJSON(t, client, server.URL+"/api/v1/xkeen/jobs/start", xkeen.CommandRequest{Action: "geodata-schedule"}, "")
	if response.StatusCode != http.StatusForbidden {
		t.Fatal(response.StatusCode)
	}
	response.Body.Close()
	response = postJSON(t, client, server.URL+"/api/v1/xkeen/jobs/start", xkeen.CommandRequest{Action: "geodata-schedule"}, csrf)
	if response.StatusCode != http.StatusAccepted {
		t.Fatal(response.StatusCode)
	}
	var job xkeen.JobView
	decodeResponse(t, response, &job)
	defer jobs.Cancel(csrf, job.ID)
	other := &http.Client{Jar: mustCookieJar(t)}
	otherCSRF := login(other)
	for _, endpoint := range []string{"read", "cancel"} {
		response = postJSON(t, other, server.URL+"/api/v1/xkeen/jobs/"+endpoint, map[string]string{"id": job.ID}, otherCSRF)
		if response.StatusCode != http.StatusConflict {
			t.Fatalf("foreign %s: %d", endpoint, response.StatusCode)
		}
		response.Body.Close()
	}
	response = postJSON(t, client, server.URL+"/api/v1/xkeen/jobs/input", map[string]string{"id": job.ID, "data": "yes\n"}, csrf)
	if response.StatusCode != http.StatusOK {
		t.Fatal(response.StatusCode)
	}
	response.Body.Close()
}
