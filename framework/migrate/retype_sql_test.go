package migrate

import "testing"

func TestRetypeCastsTextToInteger(t *testing.T) {
	cases := []struct{ from, to, dflt, want string }{
		{"text", "INTEGER", "", `ALTER TABLE "t" ALTER COLUMN "c" DROP DEFAULT, ALTER COLUMN "c" TYPE INTEGER USING "c"::INTEGER`},
		{"character varying", "BIGINT", " DEFAULT 5", `ALTER TABLE "t" ALTER COLUMN "c" DROP DEFAULT, ALTER COLUMN "c" TYPE BIGINT USING "c"::BIGINT, ALTER COLUMN "c" SET DEFAULT 5`},
		{"text", "VARCHAR(10)", " DEFAULT 'x'", `ALTER TABLE "t" ALTER COLUMN "c" TYPE VARCHAR(10)`},
		{"integer", "TEXT", "", `ALTER TABLE "t" ALTER COLUMN "c" TYPE TEXT`},
		{"integer", "BIGINT", "", `ALTER TABLE "t" ALTER COLUMN "c" TYPE BIGINT`},
	}
	for _, tc := range cases {
		if got := alterColumnTypeSQL(`"t"`, `"c"`, tc.from, tc.to, tc.dflt); got != tc.want {
			t.Errorf("%s → %s:\n got %s\nwant %s", tc.from, tc.to, got, tc.want)
		}
	}
}
