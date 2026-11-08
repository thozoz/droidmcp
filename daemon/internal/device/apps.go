package device

import (
	"errors"
	"fmt"
	"strings"
)

// MatchApp finds an app in apps by target (exact label first, then exact package,
// then case-insensitive label contains, then case-insensitive package contains).
// Ambiguity returns an error listing candidates as "Label (package)".
func MatchApp(apps []App, target string) (App, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return App{}, errors.New("empty target")
	}

	// 1. Exact label match (case-sensitive).
	var exact []App
	for _, a := range apps {
		if a.Label != "" && a.Label == target {
			exact = append(exact, a)
		}
	}
	if len(exact) == 1 {
		return exact[0], nil
	}
	if len(exact) > 1 {
		return App{}, ambiguityError(target, exact)
	}

	// 2. Exact package match.
	for _, a := range apps {
		if a.Package == target {
			return a, nil
		}
	}

	// 3. Exact label match (case-insensitive Unicode fold).
	for _, a := range apps {
		if a.Label != "" && strings.EqualFold(a.Label, target) {
			exact = append(exact, a)
		}
	}
	if len(exact) == 1 {
		return exact[0], nil
	}
	if len(exact) > 1 {
		return App{}, ambiguityError(target, exact)
	}

	// 4. Case-insensitive contains on label (standard Unicode / Locale.ROOT).
	lowerTarget := strings.ToLower(target)
	var labelMatches []App
	for _, a := range apps {
		if a.Label != "" && strings.Contains(strings.ToLower(a.Label), lowerTarget) {
			labelMatches = append(labelMatches, a)
		}
	}
	if len(labelMatches) == 1 {
		return labelMatches[0], nil
	}
	if len(labelMatches) > 1 {
		return App{}, ambiguityError(target, labelMatches)
	}

	// 5. Fallback: case-insensitive contains on package.
	var pkgMatches []App
	for _, a := range apps {
		if strings.Contains(strings.ToLower(a.Package), lowerTarget) {
			pkgMatches = append(pkgMatches, a)
		}
	}
	if len(pkgMatches) == 1 {
		return pkgMatches[0], nil
	}
	if len(pkgMatches) > 1 {
		return App{}, ambiguityError(target, pkgMatches)
	}

	return App{}, fmt.Errorf("no installed app matches %q", target)
}

func ambiguityError(target string, candidates []App) error {
	names := make([]string, len(candidates))
	for i, a := range candidates {
		if a.Label != "" {
			names[i] = fmt.Sprintf("%s (%s)", a.Label, a.Package)
		} else {
			names[i] = a.Package
		}
	}
	return fmt.Errorf("%q is ambiguous: %s", target, strings.Join(names, ", "))
}
