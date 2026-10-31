package desktop

import "fmt"

// profileShell is optional so existing shell implementations keep their zero
// behavior. Preparation happens after secondary activation forwarding, but
// before app.Start opens a listener or database. ReleaseProfile must retain a
// native-owned lease until its users have stopped, not just until GTK returns.
type profileShell interface {
	PrepareProfile(appID, dataDir string) (listenAddr string, err error)
	CommitProfile(addr string) error
	ReleaseProfile()
}

func (b *Battery) prepareProfile() (string, profileShell, error) {
	if !b.cfg.PersistentProfile {
		return "127.0.0.1:0", nil, nil
	}
	p, ok := b.shell.(profileShell)
	if !ok {
		return "", nil, fmt.Errorf("desktop: PersistentProfile is unsupported by this shell: %w", ErrUnsupported)
	}
	dir := b.dataDir
	if dir == "" {
		var err error
		dir, err = dataDirPath(b.cfg.ID)
		if err != nil {
			return "", nil, err
		}
	}
	addr, err := p.PrepareProfile(b.cfg.ID, dir)
	if err != nil {
		return "", nil, fmt.Errorf("desktop: prepare persistent browser profile: %w", err)
	}
	b.dataDir = dir
	return addr, p, nil
}
