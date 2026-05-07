package component

// LifecycleHook is a function that runs at a specific point in a component's life.
type LifecycleHook func()

// Lifecycle manages mount, update, and unmount hooks.
type Lifecycle struct {
	onMount   []LifecycleHook
	onUpdate  []LifecycleHook
	onUnmount []LifecycleHook
}

// NewLifecycle creates a new lifecycle manager.
func NewLifecycle() *Lifecycle {
	return &Lifecycle{}
}

// OnMount registers a hook to run when the component first renders.
func (l *Lifecycle) OnMount(fn LifecycleHook) {
	l.onMount = append(l.onMount, fn)
}

// OnUpdate registers a hook to run when the component re-renders.
func (l *Lifecycle) OnUpdate(fn LifecycleHook) {
	l.onUpdate = append(l.onUpdate, fn)
}

// OnUnmount registers a hook to run when the component is removed.
func (l *Lifecycle) OnUnmount(fn LifecycleHook) {
	l.onUnmount = append(l.onUnmount, fn)
}

// TriggerMount runs all mount hooks.
func (l *Lifecycle) TriggerMount() {
	for _, fn := range l.onMount {
		fn()
	}
}

// TriggerUpdate runs all update hooks.
func (l *Lifecycle) TriggerUpdate() {
	for _, fn := range l.onUpdate {
		fn()
	}
}

// TriggerUnmount runs all unmount hooks.
func (l *Lifecycle) TriggerUnmount() {
	for _, fn := range l.onUnmount {
		fn()
	}
}

// HasMountHooks returns true if any mount hooks are registered.
func (l *Lifecycle) HasMountHooks() bool {
	return len(l.onMount) > 0
}
