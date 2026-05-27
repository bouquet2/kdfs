package spdk

import (
	"context"
	"fmt"
	"strings"

	"github.com/bouquet2/kdfs/internal/names"
	spdkrpc "github.com/spdk/spdk/go/rpc/client"
)

type realClient struct {
	rpc *spdkrpc.Client
}

func NewUnixClient(socketPath string) (Client, error) {
	rpc, err := spdkrpc.CreateClientWithJsonCodec(spdkrpc.Unix, socketPath)
	if err != nil {
		return nil, fmt.Errorf("spdk rpc: create client: %w", err)
	}
	return &realClient{rpc: rpc}, nil
}

func (c *realClient) call(method string, params any) (*spdkrpc.Response, error) {
	return c.rpc.Call(method, params)
}

func (c *realClient) CreateTransport(ctx context.Context, trType string) error {
	_, err := c.call("nvmf_create_transport", map[string]any{"trtype": trType})
	if err != nil && strings.Contains(err.Error(), "already exists") {
		return nil
	}
	return err
}

func (c *realClient) CreateAIOBdev(ctx context.Context, name, filename string, blockSize int) error {
	_, err := c.call("bdev_aio_create", map[string]any{
		"name":       name,
		"filename":   filename,
		"block_size": blockSize,
	})
	if err != nil && (strings.Contains(err.Error(), "File exists") || strings.Contains(err.Error(), "already exists")) {
		c.call("bdev_aio_delete", map[string]any{"name": name})
		_, err = c.call("bdev_aio_create", map[string]any{
			"name":       name,
			"filename":   filename,
			"block_size": blockSize,
		})
	}
	return err
}

func attachNVMeControllerParams(controller NVMeController) map[string]any {
	adrFam := strings.TrimSpace(controller.AdrFam)
	return map[string]any{
		"name":    controller.Name,
		"trtype":  "tcp",
		"traddr":  controller.TrAddr,
		"trsvcid": controller.TrSvcID,
		"subnqn":  controller.SubNQN,
		"adrfam":  adrFam,
	}
}

// Attaches an NVMe controller via SPDK RPC and normalizes the response into a usable bdev name.
func (c *realClient) AttachNVMeController(ctx context.Context, controller NVMeController) (string, error) {
	resp, err := c.call("bdev_nvme_attach_controller", attachNVMeControllerParams(controller))
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			c.call("bdev_nvme_detach_controller", map[string]any{"name": controller.Name})
			resp, err = c.call("bdev_nvme_attach_controller", attachNVMeControllerParams(controller))
		}
		if err != nil {
			return "", err
		}
	}
	if resp.Result == nil {
		return "", fmt.Errorf("attach nvme controller returned nil result")
	}
	if arr, ok := resp.Result.([]any); ok && len(arr) > 0 {
		if m, ok := arr[0].(map[string]any); ok {
			if bdev, _ := m["bdev_name"].(string); bdev != "" {
				return bdev, nil
			}
			if names, ok := m["namespaces"].([]any); ok && len(names) > 0 {
				if ns, ok := names[0].(map[string]any); ok {
					if bdev, _ := ns["bdev_name"].(string); bdev != "" {
						return bdev, nil
					}
				}
			}
		}
		return controller.Name + "n1", nil
	}
	result, ok := resp.Result.(map[string]any)
	if !ok {
		return "", fmt.Errorf("unexpected result type: %T", resp.Result)
	}
	names, ok := result["namespaces"].([]any)
	if !ok || len(names) == 0 {
		return "", fmt.Errorf("no namespaces in response")
	}
	ns, ok := names[0].(map[string]any)
	if !ok {
		return "", fmt.Errorf("unexpected namespace type")
	}
	bdev, _ := ns["bdev_name"].(string)
	if bdev == "" {
		return "", fmt.Errorf("no bdev_name in namespace response")
	}
	return bdev, nil
}

func (c *realClient) CreateMirrorBdev(ctx context.Context, name string, baseBdevs []string, stripSize int) error {
	_, err := c.call("bdev_raid_create", map[string]any{
		"name":       name,
		"raid_level": "raid1",
		"base_bdevs": baseBdevs,
	})
	return err
}

func (c *realClient) CreateSubsystem(ctx context.Context, nqn, serial string, allowAnyHost bool) error {
	params := map[string]any{
		"nqn":           nqn,
		"serial_number": serial,
	}
	if allowAnyHost {
		params["allow_any_host"] = true
	}
	_, err := c.call("nvmf_create_subsystem", params)
	if err != nil && !strings.Contains(err.Error(), "already exists") && !strings.Contains(err.Error(), "Unable to create") {
		return err
	}
	return nil
}

func (c *realClient) AddNamespace(ctx context.Context, nqn string, namespace Namespace) error {
	_, err := c.call("nvmf_subsystem_add_ns", map[string]any{
		"nqn": nqn,
		"namespace": map[string]any{
			"bdev_name": namespace.BdevName,
		},
	})
	if err != nil {
		errStr := err.Error()
		if !strings.Contains(errStr, "already exists") && !strings.Contains(errStr, "Invalid parameters") {
			return err
		}
	}
	return nil
}

func (c *realClient) AddListener(ctx context.Context, nqn string, listener Listener) error {
	_, err := c.call("nvmf_subsystem_add_listener", map[string]any{
		"nqn": nqn,
		"listen_address": map[string]any{
			"trtype":  listener.TrType,
			"adrfam":  listener.AdrFam,
			"traddr":  listener.TrAddr,
			"trsvcid": listener.TrSvcID,
		},
	})
	if err != nil {
		errStr := err.Error()
		if !strings.Contains(errStr, "already exists") && !strings.Contains(errStr, "Invalid parameters") {
			return err
		}
	}
	return nil
}

func (c *realClient) DeleteSubsystem(ctx context.Context, nqn string) error {
	_, err := c.call("nvmf_delete_subsystem", map[string]any{"nqn": nqn})
	return err
}

func (c *realClient) DetachNVMeController(ctx context.Context, name string) error {
	_, err := c.call("bdev_nvme_detach_controller", map[string]any{"name": name})
	return err
}

func (c *realClient) Health(ctx context.Context) error {
	_, err := c.call("spdk_get_version", nil)
	return err
}

func (c *realClient) RemoveListener(ctx context.Context, nqn string, listener Listener) error {
	_, err := c.call("nvmf_subsystem_remove_listener", map[string]any{
		"nqn": nqn,
		"listen_address": map[string]any{
			"trtype":  listener.TrType,
			"adrfam":  listener.AdrFam,
			"traddr":  listener.TrAddr,
			"trsvcid": listener.TrSvcID,
		},
	})
	if err != nil && !strings.Contains(err.Error(), "Listener was not found") {
		return err
	}
	return nil
}

func (c *realClient) RemoveNamespace(ctx context.Context, nqn string, nsID int) error {
	_, err := c.call("nvmf_subsystem_remove_ns", map[string]any{
		"nqn":  nqn,
		"nsid": nsID,
	})
	if err != nil && !strings.Contains(err.Error(), "Namespace was not found") {
		return err
	}
	return nil
}

func (c *realClient) DestroyRAID(ctx context.Context, name string) error {
	_, err := c.call("bdev_raid_delete", map[string]any{
		"name": name,
	})
	if err != nil && !strings.Contains(err.Error(), "No such device") {
		return err
	}
	return nil
}

func (c *realClient) AddBaseBdev(ctx context.Context, raidName, baseBdev string) error {
	_, err := c.call("bdev_raid_add_base_bdev", map[string]any{
		"name":      raidName,
		"base_bdev": baseBdev,
	})
	return err
}

func (c *realClient) RemoveBaseBdev(ctx context.Context, raidName, baseBdev string) error {
	_, err := c.call("bdev_raid_remove_base_bdev", map[string]any{
		"name":      raidName,
		"base_bdev": baseBdev,
	})
	return err
}

func (c *realClient) GetRAIDInfo(ctx context.Context, name string) (map[string]any, error) {
	resp, err := c.call("bdev_raid_get_bdevs", map[string]any{
		"name":     name,
		"category": "all",
	})
	if err != nil {
		return nil, err
	}
	if arr, ok := resp.Result.([]any); ok && len(arr) > 0 {
		if m, ok := arr[0].(map[string]any); ok {
			return m, nil
		}
	}
	return nil, fmt.Errorf("RAID bdev %q not found", name)
}

func (c *realClient) AddHost(ctx context.Context, nqn, host string) error {
	if !names.IsHostNQN(host) {
		return fmt.Errorf("invalid host NQN %q", host)
	}
	_, err := c.call("nvmf_subsystem_add_host", map[string]any{
		"nqn":  nqn,
		"host": host,
	})
	if err != nil && !strings.Contains(err.Error(), "already exists") {
		return err
	}
	return nil
}
