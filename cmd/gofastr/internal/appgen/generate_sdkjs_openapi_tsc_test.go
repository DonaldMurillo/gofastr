package appgen

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// oaTSUse calls the fixture's client.d.ts the way a TypeScript caller
// would, so tsc checks the declarations and the shapes they promise.
const oaTSUse = `import { Client, APIPrefix, ApiError, type GenerateRequest, type ValidateResult } from "./client.js";

// A JSON operation resolves to null on an empty answer, so its type must
// admit null: true only when it does.
type AdmitsNull<T> = null extends T ? true : false;
export const nullable: [AdmitsNull<Awaited<ReturnType<Client["validateData"]>>>, AdmitsNull<Awaited<ReturnType<Client["uploadOpt"]>>>] = [true, true];

const api = new Client({ baseURL: "https://barc.example.com" + APIPrefix, token: "t", maxBlobBytes: 1024 });

export async function use(): Promise<void> {
  const req: GenerateRequest = { data: "x", symbology: "qr" };
  const png: Blob = await api.generateBarcode(req);
  const zip: Blob = await api.batchZip([req]);
  const v: ValidateResult | null = await api.validateData({ data: "d" });
  if (v !== null) {
    const ok: boolean = v.valid;
    const serial: number | string | undefined = v.serial;
    void ok;
    void serial;
  }
  await api.validateData();
  const spec = await api.getSpec({ symbology: "qr", limit: 1, "X-Trace": "t" });
  const lengths = spec?.lengths;
  await api.postOpt(undefined, { width: 1 });
  await api.postOpt({ dryRun: true }, { width: 1 });
  const n: number | string | null = await api.uploadOpt(undefined, new Blob(["x"]));
  const gone: null = await api.ping();
  void png; void zip; void lengths; void n; void gone;
  try {
    await api.do("GET", "/x");
  } catch (e) {
    if (e instanceof ApiError) {
      const status: number = e.status;
      void status;
    }
  }
}
`

// The generated client.d.ts passes tsc --strict, including an operation
// whose optional parameters come before a required body (TS1016 when the
// parameter object is declared optional), and its JSON results admit the
// null an empty answer resolves to. tsc comes from the machine's PATH: the
// repo has no node toolchain of its own, so the test skips without one.
func TestOpenAPISDKTypesCompile(t *testing.T) {
	tsc, err := exec.LookPath("tsc")
	if err != nil {
		if os.Getenv("GOFASTR_REQUIRE_TSC") != "" {
			t.Fatal("tsc is not on PATH and GOFASTR_REQUIRE_TSC is set")
		}
		t.Skip("tsc is not on PATH")
	}
	out := oaSDKGenerate(t, oaSDKFixture)
	dir := t.TempDir()
	dts, err := os.ReadFile(filepath.Join(out, "js", "client.d.ts"))
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"client.d.ts": string(dts), "use.ts": oaTSUse} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(tsc, "--strict", "--noEmit", "--target", "es2022", "--lib", "es2022,dom",
		"--module", "esnext", "--moduleResolution", "bundler", "client.d.ts", "use.ts")
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("tsc over the generated client.d.ts: %v\n%s\n%s", err, output, dts)
	}
}
