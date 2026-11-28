package embedcheck

import "testing"

func TestCheckAllDefaultsToEveryPackage(t *testing.T) {
	t.Chdir(testdataDir(t))
	findings, _, _, _ := CheckAll()
	for _, f := range findings {
		if f.Surface == "bad" {
			return
		}
	}
	t.Fatalf("CheckAll with no pattern did not scan ./...: %+v", findings)
}

func TestCheckAllChecksEachPackageOnce(t *testing.T) {
	t.Chdir(testdataDir(t))
	findings, _, _, err := CheckAll("./src/bad", "./src/bad")
	if err != nil || len(findings) != 1 {
		t.Fatalf("findings=%+v err=%v; want the one finding once", findings, err)
	}
}

func TestCheckSkipsTestFiles(t *testing.T) {
	t.Chdir(testdataDir(t))
	findings, _, err := Check("./src/testonly")
	if err != nil || len(findings) != 0 {
		t.Fatalf("a surface declared in a _test.go file was checked: findings=%+v err=%v", findings, err)
	}
}
