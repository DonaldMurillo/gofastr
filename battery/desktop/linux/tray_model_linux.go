//go:build linux && (amd64 || arm64)

package linux

import (
	"bytes"
	"context"
	"errors"
	"image/color"
	"image/png"
	"strings"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
)

const (
	trayTextLimit  = 4096
	trayNodeLimit  = 128
	trayDepthLimit = 8
	trayIconLimit  = 1 << 20
)

type trayNode struct {
	id       int32
	plan     desktop.MenuItemPlan
	actionID string
	children []*trayNode
}

func (n *trayNode) enabled() bool {
	return !n.plan.Sep && (len(n.children) > 0 || n.actionID != "" || n.plan.Role != "")
}

type trayModel struct {
	id, title, tooltip string
	width, height      int
	argb               []byte
	root               *trayNode
	nodes              map[int32]*trayNode
}

func validTrayText(s string) bool {
	return len(s) <= trayTextLimit && utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}

func newTrayModel(ctx context.Context, cfg desktop.WindowConfig) (*trayModel, error) {
	if cfg.Tray == nil {
		return nil, errors.New("desktop/linux: no tray configured")
	}
	if !validTrayText(cfg.AppID) || !validTrayText(cfg.Title) || !validTrayText(cfg.Tray.Title) {
		return nil, errors.New("desktop/linux: invalid tray identity/title")
	}
	// Bound the raw tree before the planner recurses; direct Shell users need
	// the same protection even if Battery.New did not validate their menu.
	count := 0
	var check func([]desktop.MenuItem, int) error
	check = func(items []desktop.MenuItem, depth int) error {
		if depth > trayDepthLimit {
			return errors.New("desktop/linux: tray menu is too deep")
		}
		for _, item := range items {
			count++
			if count > trayNodeLimit-4 {
				return errors.New("desktop/linux: tray menu has too many items")
			}
			if err := check(item.Children, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	if cfg.Tray.Menu != nil {
		if err := check(cfg.Tray.Menu.Items, 0); err != nil {
			return nil, err
		}
	}
	m := &trayModel{id: cfg.AppID, title: cfg.Tray.Title, tooltip: cfg.Tray.Tooltip.Text(ctx), nodes: make(map[int32]*trayNode)}
	if m.id == "" {
		m.id = "gofastr"
	}
	if !validTrayText(m.tooltip) {
		return nil, errors.New("desktop/linux: invalid tray tooltip")
	}
	var err error
	m.width, m.height, m.argb, err = trayIcon(cfg.Tray.Icon)
	if err != nil {
		return nil, err
	}
	var ids, recoveryIDs []string
	rows := desktop.PlanTrayMenu(ctx, cfg.Tray, cfg.Title, &ids)
	// Recovery actions are always top-level, even when the app put its own
	// Show/Quit inside a submenu. An empty menu must never strand a window.
	standard := &desktop.Tray{Menu: &desktop.Menu{Items: []desktop.MenuItem{{Role: desktop.RoleShow}, {Role: desktop.RoleQuit}}}}
	recovery := desktop.PlanTrayMenu(ctx, standard, cfg.Title, &recoveryIDs)
	if len(rows) > 0 {
		rows = append(rows, desktop.MenuItemPlan{Sep: true})
	}
	rows = append(rows, recovery...)
	m.root = &trayNode{id: 0}
	m.nodes[0] = m.root
	var build func(*trayNode, []desktop.MenuItemPlan) error
	build = func(parent *trayNode, rows []desktop.MenuItemPlan) error {
		for _, row := range rows {
			if !validTrayText(row.Title) {
				return errors.New("desktop/linux: invalid tray menu title")
			}
			n := &trayNode{id: int32(len(m.nodes)), plan: row}
			if row.Action && row.Tag >= 0 && row.Tag < len(ids) {
				n.actionID = ids[row.Tag]
			}
			m.nodes[n.id] = n
			parent.children = append(parent.children, n)
			if err := build(n, row.Submenu); err != nil {
				return err
			}
		}
		return nil
	}
	if err := build(m.root, rows); err != nil {
		return nil, err
	}
	return m, nil
}

func trayIcon(data []byte) (int, int, []byte, error) {
	if len(data) == 0 {
		out := make([]byte, 24*24*4)
		for y := 0; y < 24; y++ {
			for x := 0; x < 24; x++ {
				i := (y*24 + x) * 4
				out[i] = 255
				v := byte(32)
				if x >= 5 && x < 19 && y >= 5 && y < 19 {
					v = 255
				}
				out[i+1] = v
				out[i+2] = v
				out[i+3] = v
			}
		}
		return 24, 24, out, nil
	}
	if len(data) > trayIconLimit {
		return 0, 0, nil, errors.New("desktop/linux: tray PNG exceeds 1 MiB")
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 256 || cfg.Height > 256 {
		return 0, 0, nil, errors.New("desktop/linux: tray icon must be a valid PNG no larger than 256x256")
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return 0, 0, nil, errors.New("desktop/linux: invalid tray PNG")
	}
	out := make([]byte, cfg.Width*cfg.Height*4)
	for y := 0; y < cfg.Height; y++ {
		for x := 0; x < cfg.Width; x++ {
			c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			i := (y*cfg.Width + x) * 4
			// SNI uses non-premultiplied, network-order ARGB32.
			out[i] = c.A
			out[i+1] = c.R
			out[i+2] = c.G
			out[i+3] = c.B
		}
	}
	return cfg.Width, cfg.Height, out, nil
}
