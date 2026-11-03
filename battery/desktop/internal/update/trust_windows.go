//go:build windows

package update

import (
	"errors"
	"fmt"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modWintrust                    = windows.NewLazySystemDLL("wintrust.dll")
	procWTHelperProvDataFromState  = modWintrust.NewProc("WTHelperProvDataFromStateData")
	procWTHelperGetProvSignerChain = modWintrust.NewProc("WTHelperGetProvSignerFromChain")
	procWTHelperGetProvCertChain   = modWintrust.NewProc("WTHelperGetProvCertFromChain")
)

// cryptProviderCert is the head of CRYPT_PROVIDER_CERT: cbStruct, then
// the certificate context. Go lays the two out as C does on every
// Windows architecture.
type cryptProviderCert struct {
	size uint32
	cert *windows.CertContext
}

// VerifyAuthenticode runs WinVerifyTrust (WINTRUST_ACTION_GENERIC_VERIFY_V2)
// on the file at path with no UI and no revocation fetch, and reads the
// signer certificate's subject from the verified state. Revocation is
// off because an install must not hang on a CRL download; the feed's
// ed25519 signature is the primary check.
func VerifyAuthenticode(path string) Trust {
	wpath, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return Trust{State: Untrusted, Detail: "path is not valid UTF-16"}
	}
	file := windows.WinTrustFileInfo{FilePath: wpath}
	file.Size = uint32(unsafe.Sizeof(file))
	data := windows.WinTrustData{
		UIChoice:                        windows.WTD_UI_NONE,
		RevocationChecks:                windows.WTD_REVOKE_NONE,
		UnionChoice:                     windows.WTD_CHOICE_FILE,
		FileOrCatalogOrBlobOrSgnrOrCert: unsafe.Pointer(&file),
		StateAction:                     windows.WTD_STATEACTION_VERIFY,
		ProvFlags:                       windows.WTD_REVOCATION_CHECK_NONE | windows.WTD_CACHE_ONLY_URL_RETRIEVAL | windows.WTD_DISABLE_MD2_MD4,
	}
	data.Size = uint32(unsafe.Sizeof(data))
	verr := windows.WinVerifyTrustEx(windows.InvalidHWND, &windows.WINTRUST_ACTION_GENERIC_VERIFY_V2, &data)
	subject := signerSubject(data.StateData)
	data.StateAction = windows.WTD_STATEACTION_CLOSE
	_ = windows.WinVerifyTrustEx(windows.InvalidHWND, &windows.WINTRUST_ACTION_GENERIC_VERIFY_V2, &data)
	runtime.KeepAlive(wpath)
	runtime.KeepAlive(&file)

	if verr == nil {
		if len(subject) == 0 {
			return Trust{State: Untrusted, Detail: "verified signature has no readable signer"}
		}
		return Trust{State: Trusted, Subject: subject}
	}
	var errno syscall.Errno
	if errors.As(verr, &errno) && uint32(errno) == uint32(windows.TRUST_E_NOSIGNATURE) {
		return Trust{State: Unsigned, Detail: "no signature"}
	}
	return Trust{State: Untrusted, Subject: subject, Detail: trustDetail(verr)}
}

// signerSubject copies the first signer's certificate subject (DER)
// out of WinVerifyTrust's state, nil when there is none.
func signerSubject(state windows.Handle) []byte {
	if state == 0 {
		return nil
	}
	//gofastr:allow(discardederr) the WTHelper functions report failure by returning nil; Call's errno is a stale GetLastError
	prov, _, _ := procWTHelperProvDataFromState.Call(uintptr(state))
	if prov == 0 {
		return nil
	}
	//gofastr:allow(discardederr) the WTHelper functions report failure by returning nil; Call's errno is a stale GetLastError
	sgnr, _, _ := procWTHelperGetProvSignerChain.Call(prov, 0, 0, 0)
	if sgnr == 0 {
		return nil
	}
	//gofastr:allow(discardederr) the WTHelper functions report failure by returning nil; Call's errno is a stale GetLastError
	pc, _, _ := procWTHelperGetProvCertChain.Call(sgnr, 0)
	if pc == 0 {
		return nil
	}
	// pc points into wintrust's own allocation, not Go memory; reading
	// it through a pointer-to-uintptr keeps vet's uintptr rule intact.
	provCert := *(**cryptProviderCert)(unsafe.Pointer(&pc))
	if provCert.cert == nil || provCert.cert.CertInfo == nil {
		return nil
	}
	name := provCert.cert.CertInfo.Subject
	if name.Data == nil || name.Size == 0 || name.Size > 64<<10 {
		return nil
	}
	return append([]byte(nil), unsafe.Slice(name.Data, name.Size)...)
}

// trustDetail names a WinVerifyTrust failure by its HRESULT, the form
// signtool and Get-AuthenticodeSignature documentation use.
func trustDetail(err error) string {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		switch uint32(errno) {
		case 0x800B0109:
			return "CERT_E_UNTRUSTEDROOT"
		case 0x80096010:
			return "TRUST_E_BAD_DIGEST"
		case 0x800B0101:
			return "CERT_E_EXPIRED"
		case 0x800B010A:
			return "CERT_E_CHAINING"
		case 0x800B0004:
			return "TRUST_E_SUBJECT_NOT_TRUSTED"
		}
		return fmt.Sprintf("HRESULT 0x%08X", uint32(errno))
	}
	return "verification failed"
}
