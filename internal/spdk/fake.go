package spdk

import "context"

type FakeClient struct {
	Calls []string
	Err   error
}

func (f *FakeClient) record(call string) error {
	f.Calls = append(f.Calls, call)
	return f.Err
}

func (f *FakeClient) CreateTransport(ctx context.Context, trType string) error {
	return f.record("CreateTransport:" + trType)
}

func (f *FakeClient) CreateAIOBdev(ctx context.Context, name, filename string, blockSize int) error {
	return f.record("CreateAIOBdev:" + name + ":" + filename)
}

func (f *FakeClient) AttachNVMeController(ctx context.Context, controller NVMeController) (string, error) {
	if err := f.record("AttachNVMeController:" + controller.Name + ":" + controller.SubNQN + ":" + controller.AdrFam + ":" + controller.TrAddr); err != nil {
		return "", err
	}
	return controller.Name + "n1", nil
}

func (f *FakeClient) CreateMirrorBdev(ctx context.Context, name string, baseBdevs []string, stripSize int) error {
	return f.record("CreateMirrorBdev:" + name)
}

func (f *FakeClient) CreateSubsystem(ctx context.Context, nqn, serial string, allowAnyHost bool) error {
	return f.record("CreateSubsystem:" + nqn)
}

func (f *FakeClient) AddNamespace(ctx context.Context, nqn string, namespace Namespace) error {
	return f.record("AddNamespace:" + namespace.BdevName)
}

func (f *FakeClient) AddListener(ctx context.Context, nqn string, listener Listener) error {
	return f.record("AddListener:" + listener.TrSvcID + ":" + listener.AdrFam + ":" + listener.TrAddr)
}

func (f *FakeClient) AddHost(ctx context.Context, nqn, host string) error {
	return f.record("AddHost:" + nqn + ":" + host)
}

func (f *FakeClient) DeleteSubsystem(ctx context.Context, nqn string) error {
	return f.record("DeleteSubsystem:" + nqn)
}
func (f *FakeClient) DetachNVMeController(ctx context.Context, name string) error {
	return f.record("DetachNVMeController:" + name)
}
func (f *FakeClient) Health(ctx context.Context) error { return f.record("Health") }

func (f *FakeClient) RemoveListener(ctx context.Context, nqn string, listener Listener) error {
	return f.record("RemoveListener:" + nqn)
}

func (f *FakeClient) RemoveNamespace(ctx context.Context, nqn string, nsID int) error {
	return f.record("RemoveNamespace:" + nqn)
}

func (f *FakeClient) DestroyRAID(ctx context.Context, name string) error {
	return f.record("DestroyRAID:" + name)
}

func (f *FakeClient) AddBaseBdev(ctx context.Context, raidName, baseBdev string) error {
	return f.record("AddBaseBdev:" + raidName + ":" + baseBdev)
}

func (f *FakeClient) RemoveBaseBdev(ctx context.Context, raidName, baseBdev string) error {
	return f.record("RemoveBaseBdev:" + raidName + ":" + baseBdev)
}

func (f *FakeClient) GetRAIDInfo(ctx context.Context, name string) (map[string]any, error) {
	if err := f.record("GetRAIDInfo:" + name); err != nil {
		return nil, err
	}
	return map[string]any{}, nil
}
