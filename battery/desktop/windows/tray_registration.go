package windows

type trayRegistration struct {
	live           bool
	closeHides     bool
	taskbarCreated uint32
	add            func() bool
	visible        func() bool
	show           func()
}

func (state *trayRegistration) register() {
	state.live = state.add()
}

func (state *trayRegistration) hideOnClose(mainWindow bool, runErr error) bool {
	return mainWindow && state.closeHides && runErr == nil && state.live
}

func (state *trayRegistration) handleMessage(message uint32, mainWindow bool) bool {
	if message == 0 || message != state.taskbarCreated || !mainWindow {
		return false
	}
	state.register()
	if !state.live && state.closeHides && !state.visible() {
		state.show()
	}
	return true
}
