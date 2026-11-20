package featureflag

import (
	"errors"
	"net/url"
	"strings"
)

var tenantEscaper = strings.NewReplacer("%", "%25", ":", "%3A")

// UserEntry spells a [Flag.Users] entry: "tenant:user" with the tenant
// percent-escaped, or the bare user id (":user" if it holds ':') with none.
func UserEntry(tenantID, userID string) string {
	if tenantID == "" {
		if strings.Contains(userID, ":") {
			return ":" + userID
		}
		return userID
	}
	return tenantEscaper.Replace(tenantID) + ":" + userID
}

// ParseUserEntry inverts [UserEntry], refusing an empty user or bad escape.
func ParseUserEntry(entry string) (tenantID, userID string, err error) {
	tenantPart, userID, qualified := strings.Cut(entry, ":")
	if !qualified {
		tenantPart, userID = "", entry
	}
	if userID == "" {
		return "", "", errors.New("featureflag: user entry names no user id")
	}
	tenantID, err = url.PathUnescape(tenantPart)
	if err != nil {
		return "", "", errors.New("featureflag: user entry has a malformed tenant escape")
	}
	return tenantID, userID, nil
}

func userAllowed(users []string, tenantID, userID string) bool {
	for _, u := range users {
		t, id, err := ParseUserEntry(u)
		if err == nil && t == tenantID && id == userID {
			return true
		}
	}
	return false
}
