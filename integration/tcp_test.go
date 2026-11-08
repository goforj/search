//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"github.com/docker/go-connections/nat"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"io"
	"strings"
	"testing"
	"time"
)

// startTCPServer owns one authenticated database container for all tests of that engine.
func startTCPServer(t *testing.T, engine string) string {
	t.Helper()
	fixturePool.Lock()
	defer fixturePool.Unlock()
	if endpoint, ok := fixturePool.endpoints[engine]; ok {
		return endpoint
	}
	var versions map[string]engineVersion
	if err := json.Unmarshal(pinnedVersions, &versions); err != nil {
		t.Fatal(err)
	}
	request := testcontainers.ContainerRequest{Image: versions[engine].Image}
	port := "5432/tcp"
	if engine == "postgres" {
		request.Env = map[string]string{"POSTGRES_USER": "search", "POSTGRES_PASSWORD": testAPIKey, "POSTGRES_DB": "search"}
		request.WaitingFor = wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(60 * time.Second)
	} else if engine == "redis" {
		port = "6379/tcp"
		request.Cmd = []string{"redis-server", "--requirepass", testAPIKey, "--save", "", "--appendonly", "no"}
		request.WaitingFor = wait.ForListeningPort(nat.Port(port)).WithStartupTimeout(60 * time.Second)
	} else {
		t.Fatal("unknown TCP engine")
	}
	request.ExposedPorts = []string{port}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{ContainerRequest: request, Started: true})
	if container != nil {
		fixturePool.containers = append(fixturePool.containers, container)
		fixturePool.engines[engine] = container
	}
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := container.PortEndpoint(ctx, nat.Port(port), "")
	if err != nil {
		t.Fatal(err)
	}
	if engine == "redis" {
		code, output, err := container.Exec(ctx, []string{"redis-cli", "--no-auth-warning", "-a", testAPIKey, "--raw", "INFO", "server"})
		if err != nil {
			t.Fatal("cannot verify native Redis version")
		}
		data, err := io.ReadAll(output)
		if err != nil || code != 0 || !strings.Contains(string(data), "redis_version:"+versions[engine].Version) {
			t.Fatal("Redis version differs from pinned fixture")
		}
	}
	fixturePool.endpoints[engine] = endpoint
	t.Logf("started shared %s container", engine)
	return endpoint
}
