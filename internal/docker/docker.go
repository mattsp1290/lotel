package docker

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"

	"github.com/mattsp1290/lotel/internal/config"
)

type Client struct {
	docker *client.Client
}

func NewClient() (*Client, error) {
	c, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("creating docker client: %w", err)
	}
	return &Client{docker: c}, nil
}

func (c *Client) Close() error {
	return c.docker.Close()
}

func (c *Client) EnsureNetwork(ctx context.Context, name string) error {
	networks, err := c.docker.NetworkList(ctx, network.ListOptions{})
	if err != nil {
		return fmt.Errorf("listing networks: %w", err)
	}
	for _, n := range networks {
		if n.Name == name {
			return nil
		}
	}
	_, err = c.docker.NetworkCreate(ctx, name, network.CreateOptions{
		Driver: "bridge",
	})
	if err != nil {
		return fmt.Errorf("creating network %s: %w", name, err)
	}
	fmt.Printf("Created network: %s\n", name)
	return nil
}

func (c *Client) StartCollector(ctx context.Context, configPath, dataPath string) error {
	// Pull image
	fmt.Printf("Pulling image: %s\n", config.ImageName)
	reader, err := c.docker.ImagePull(ctx, config.ImageName, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("pulling image: %w", err)
	}
	io.Copy(io.Discard, reader)
	reader.Close()

	// Check if container already exists
	containers, err := c.docker.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return fmt.Errorf("listing containers: %w", err)
	}
	for _, ctr := range containers {
		for _, name := range ctr.Names {
			if name == "/"+config.ContainerName {
				if ctr.State == "running" {
					fmt.Println("Collector is already running.")
					return nil
				}
				// Remove stopped container
				fmt.Println("Removing stopped collector container...")
				if err := c.docker.ContainerRemove(ctx, ctr.ID, container.RemoveOptions{}); err != nil {
					return fmt.Errorf("removing stopped container: %w", err)
				}
			}
		}
	}

	// Ensure network
	if err := c.EnsureNetwork(ctx, config.NetworkName); err != nil {
		return err
	}

	// Build port bindings
	exposedPorts := nat.PortSet{}
	portBindings := nat.PortMap{}
	for _, p := range config.Ports {
		port := nat.Port(p + "/tcp")
		exposedPorts[port] = struct{}{}
		portBindings[port] = []nat.PortBinding{{HostIP: "0.0.0.0", HostPort: p}}
	}

	// Create container
	resp, err := c.docker.ContainerCreate(ctx,
		&container.Config{
			Image:        config.ImageName,
			Cmd:          []string{"--config=/etc/otel-collector-config.yaml"},
			ExposedPorts: exposedPorts,
			Healthcheck: &container.HealthConfig{
				Test:     []string{"CMD", "curl", "-f", "http://localhost:13133/"},
				Interval: 30 * time.Second,
				Timeout:  10 * time.Second,
				Retries:  3,
			},
		},
		&container.HostConfig{
			PortBindings: portBindings,
			Binds: []string{
				configPath + ":/etc/otel-collector-config.yaml",
				dataPath + ":/data",
			},
			RestartPolicy: container.RestartPolicy{Name: "unless-stopped"},
		},
		nil, nil, config.ContainerName,
	)
	if err != nil {
		return fmt.Errorf("creating container: %w", err)
	}

	// Connect to network
	if err := c.docker.NetworkConnect(ctx, config.NetworkName, resp.ID, nil); err != nil {
		return fmt.Errorf("connecting to network: %w", err)
	}

	// Start container
	if err := c.docker.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return fmt.Errorf("starting container: %w", err)
	}

	fmt.Println("Collector started successfully.")
	fmt.Printf("Data directory: %s\n", dataPath)
	fmt.Println("Ports:")
	for _, p := range config.Ports {
		fmt.Printf("  - %s -> %s\n", p, p)
	}
	return nil
}

func (c *Client) StopCollector(ctx context.Context) error {
	containers, err := c.docker.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return fmt.Errorf("listing containers: %w", err)
	}

	for _, ctr := range containers {
		for _, name := range ctr.Names {
			if name == "/"+config.ContainerName {
				if ctr.State == "running" {
					fmt.Println("Stopping collector...")
					timeout := 10
					if err := c.docker.ContainerStop(ctx, ctr.ID, container.StopOptions{Timeout: &timeout}); err != nil {
						return fmt.Errorf("stopping container: %w", err)
					}
				}
				fmt.Println("Removing collector container...")
				if err := c.docker.ContainerRemove(ctx, ctr.ID, container.RemoveOptions{}); err != nil {
					return fmt.Errorf("removing container: %w", err)
				}
				fmt.Println("Collector stopped and removed.")
				return nil
			}
		}
	}

	fmt.Println("No collector container found.")
	return nil
}
