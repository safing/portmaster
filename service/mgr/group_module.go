package mgr

// GroupModule is a module that wraps a group of modules,
// to allow nesting groups of modules in parent group.
type GroupModule struct {
	mgr   *Manager
	group *Group
}

func NewGroupModule(name string, modules ...Module) *GroupModule {
	return &GroupModule{
		mgr:   New(name),
		group: NewGroup(modules...),
	}
}

func (gm *GroupModule) Manager() *Manager {
	return gm.mgr
}

func (gm *GroupModule) Start() error {
	return gm.group.Start()
}

func (gm *GroupModule) Stop() error {
	return gm.group.Stop()
}

// Modules returns the modules in the group wrapped by this group module.
// (mimics Group.Modules())
func (gm *GroupModule) Modules() []Module {
	return gm.group.Modules()
}
