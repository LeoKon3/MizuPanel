package main

import (
	"net/http"
	"testing"
)

func TestReleasePathsResolveNextToRootExecutable(t *testing.T) {
	paths := releasePaths("/opt/mizupanel/mizupanel-server")

	if paths.StaticDir != "/opt/mizupanel/web" {
		t.Fatalf("StaticDir = %q, want /opt/mizupanel/web", paths.StaticDir)
	}
	if paths.DownloadDir != "/opt/mizupanel/downloads" {
		t.Fatalf("DownloadDir = %q, want /opt/mizupanel/downloads", paths.DownloadDir)
	}
}

func TestRuntimeReleasePathsUseExecutablePath(t *testing.T) {
	paths, err := runtimeReleasePaths(func() (string, error) {
		return "/opt/mizupanel/mizupanel-server", nil
	})
	if err != nil {
		t.Fatalf("runtimeReleasePaths returned error: %v", err)
	}
	if paths.StaticDir != "/opt/mizupanel/web" || paths.DownloadDir != "/opt/mizupanel/downloads" {
		t.Fatalf("paths = %#v", paths)
	}
}

func TestNewHTTPServerConfiguresLifecycleBoundaries(t *testing.T) {
	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	server := newHTTPServer(":8080", handler)

	if server.Addr != ":8080" || server.Handler == nil {
		t.Fatalf("server wiring = %#v", server)
	}
	if server.ReadHeaderTimeout != serverReadHeaderTimeout {
		t.Fatalf("ReadHeaderTimeout = %s, want %s", server.ReadHeaderTimeout, serverReadHeaderTimeout)
	}
	if server.IdleTimeout != serverIdleTimeout {
		t.Fatalf("IdleTimeout = %s, want %s", server.IdleTimeout, serverIdleTimeout)
	}
	if server.MaxHeaderBytes != serverMaxHeaderBytes {
		t.Fatalf("MaxHeaderBytes = %d, want %d", server.MaxHeaderBytes, serverMaxHeaderBytes)
	}
}
