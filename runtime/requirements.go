package runtime

import (
	"errors"
	"fmt"

	"github.com/d7z-team/mini-go/runtime/bytecode"
)

func bindModuleRequirements(root *moduleInstance, modules *moduleRegistry) error {
	return bindExecutableRequirements(root, modules, map[string]struct{}{}, map[string]struct{}{})
}

func bindExecutableRequirements(module *moduleInstance, modules *moduleRegistry, visited, visiting map[string]struct{}) error {
	if module == nil || module.executable == nil {
		return errors.New("nil executable")
	}
	executable := module.executable
	modulePath := executable.Artifact.Module.Path
	if modulePath != "" {
		if _, ok := visited[modulePath]; ok {
			return nil
		}
		if _, ok := visiting[modulePath]; ok {
			return fmt.Errorf("module dependency cycle at %q", modulePath)
		}
		visiting[modulePath] = struct{}{}
		defer delete(visiting, modulePath)
	}
	for _, requirement := range executable.Artifact.Requirements {
		if requirement.Kind != bytecode.RequirementSource {
			continue
		}
		dependency, ok := modules.module(requirement.ModulePath)
		if !ok {
			return fmt.Errorf("missing module %q", requirement.ModulePath)
		}
		if requirement.Hash != "" && dependency.executable.Hash != requirement.Hash {
			return fmt.Errorf("module %q hash mismatch", requirement.ModulePath)
		}
		for _, exportName := range requirement.Exports {
			if _, ok := dependency.executable.Exports[exportName]; !ok && !dependency.executable.hasType(exportName) {
				return fmt.Errorf("module %q missing export %q", requirement.ModulePath, exportName)
			}
		}
		if err := bindExecutableRequirements(dependency, modules, visited, visiting); err != nil {
			return err
		}
	}
	if modulePath != "" {
		visited[modulePath] = struct{}{}
	}
	return nil
}
