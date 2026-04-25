package extract

// SetValidateURLForTest replaces the package-level URL guard with fn and
// returns a function that restores the original. It exists solely so test
// code (in this package or others) can exercise the extract worker against
// httptest servers bound to 127.0.0.1, which the production guard rejects.
// Production code must never call this.
func SetValidateURLForTest(fn func(string) error) (restore func()) {
	prev := validateURLFn
	validateURLFn = fn
	return func() { validateURLFn = prev }
}
