package spdk

import "context"

type NVMeController struct {
	Name    string
	TrAddr  string
	TrSvcID string
	SubNQN  string
	AdrFam  string
}

type Namespace struct {
	BdevName string
}

type Listener struct {
	TrType  string
	AdrFam  string
	TrAddr  string
	TrSvcID string
}

type Client interface {
	CreateTransport(ctx context.Context, trType string) error
	CreateAIOBdev(ctx context.Context, name, filename string, blockSize int) error
	AttachNVMeController(ctx context.Context, controller NVMeController) (string, error)
	CreateMirrorBdev(ctx context.Context, name string, baseBdevs []string, stripSize int) error
	CreateSubsystem(ctx context.Context, nqn, serial string, allowAnyHost bool) error
	AddHost(ctx context.Context, nqn, host string) error
	AddNamespace(ctx context.Context, nqn string, namespace Namespace) error
	AddListener(ctx context.Context, nqn string, listener Listener) error
	DeleteSubsystem(ctx context.Context, nqn string) error
	DetachNVMeController(ctx context.Context, name string) error
	Health(ctx context.Context) error
	RemoveListener(ctx context.Context, nqn string, listener Listener) error
	RemoveNamespace(ctx context.Context, nqn string, nsID int) error
	DestroyRAID(ctx context.Context, name string) error
	AddBaseBdev(ctx context.Context, raidName, baseBdev string) error
	RemoveBaseBdev(ctx context.Context, raidName, baseBdev string) error
	GetRAIDInfo(ctx context.Context, name string) (map[string]any, error)
}
