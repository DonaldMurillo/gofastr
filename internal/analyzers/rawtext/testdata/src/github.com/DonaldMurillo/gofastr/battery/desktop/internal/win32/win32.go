package win32

func SetWindowText(hwnd uintptr, title string) error { return nil }

func AppendMenu(menu uintptr, flags uint32, idOrSubmenu uintptr, title string) error { return nil }

func MessageBox(hwnd uintptr, text, caption string, flags uint32) int32 { return 0 }
