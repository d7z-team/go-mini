package workspace

import (
	"fmt"

	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/target"
)

// SelectPackage applies build constraints before header scanning or parsing.
func SelectPackage(pkg SourcePackage, buildTarget target.Target) (SourcePackage, []source.Diagnostic, error) {
	normalizedTarget, err := target.Normalize(buildTarget)
	if err != nil {
		return SourcePackage{}, nil, err
	}
	pkg, err = normalizePackage(pkg)
	if err != nil {
		return SourcePackage{}, nil, err
	}
	if len(pkg.SourceCandidates) != 0 {
		if !pkg.SelectionTarget.Equal(normalizedTarget) {
			return SourcePackage{}, nil, fmt.Errorf("source package %q was selected for a different target", pkg.ModulePath)
		}
		return pkg, nil, nil
	}

	out := SourcePackage{ID: pkg.ID, ModulePath: pkg.ModulePath, SelectionTarget: normalizedTarget, Resources: append([]ResourceFile(nil), pkg.Resources...)}
	var diagnostics []source.Diagnostic
	selectFiles := func(files []source.File, tests bool) {
		for _, file := range files {
			if file.Hash == "" {
				file.Hash = source.HashText(file.Text)
			}
			selected, matchErr := target.MatchSource(file.Text, normalizedTarget)
			candidateHash := file.Hash
			if file.OriginPath != "" {
				candidateHash = source.HashText(file.OriginPath + "\x00" + file.Hash)
			}
			out.SourceCandidates = append(out.SourceCandidates, SourceCandidate{
				Path: file.Path, Hash: candidateHash, Size: len(file.Text), Selected: selected && matchErr == nil, Test: tests,
			})
			if matchErr != nil {
				diagnostic := workspaceDiagnostic("compiler.target.constraint", matchErr.Error())
				if constraintErr, ok := matchErr.(target.ConstraintError); ok {
					if span, valid := file.Span(constraintErr.Offset, constraintErr.Offset); valid {
						diagnostic.Primary = span
					}
				}
				diagnostics = append(diagnostics, diagnostic)
				continue
			}
			if selected {
				if tests {
					out.TestFiles = append(out.TestFiles, file)
				} else {
					out.Files = append(out.Files, file)
				}
			}
		}
	}
	selectFiles(pkg.Files, false)
	selectFiles(pkg.TestFiles, true)
	if len(out.Files) == 0 && !source.HasErrors(diagnostics) {
		diagnostics = append(diagnostics, workspaceDiagnostic("compiler.target.no_files", fmt.Sprintf("source package %q has no files for build tags", pkg.ModulePath)))
	}
	return out, source.NormalizeDiagnostics(diagnostics), nil
}
