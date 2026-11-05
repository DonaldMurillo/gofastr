package windows

type trayRegistration struct {
	live bool
}

func (state *trayRegistration) register(add func() bool) {
	state.live = add()
}
