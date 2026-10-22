package migrate

import "testing"

func TestRetypeCastsTextToInteger(t *testing.T) {
	cases := []struct{ from, to, want string }{
		{"text", "INTEGER", `ALTER TABLE "t" ALTER COLUMN "c" TYPE INTEGER USING "c"::INTEGER`},
		{"character varying", "BIGINT", `ALTER TABLE "t" ALTER COLUMN "c" TYPE BIGINT USING "c"::BIGINT`},
		{"text", "VARCHAR(10)", `ALTER TABLE "t" ALTER COLUMN "c" TYPE VARCHAR(10)`},
		{"integer", "TEXT", `ALTER TABLE "t" ALTER COLUMN "c" TYPE TEXT`},
		{"integer", "BIGINT", `ALTER TABLE "t" ALTER COLUMN "c" TYPE BIGINT`},
	}
	for _, tc := range cases {
		if got := alterColumnTypeSQL(`"t"`, `"c"`, tc.from, tc.to); got != tc.want {
			t.Errorf("%s → %s:\n got %s\nwant %s", tc.from, tc.to, got, tc.want)
		}
	}
}
