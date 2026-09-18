package lower

import "strings"

func (l *lowerer) pushBranchWithUserLabel(userLabel, breakLabel, continueLabel string, fallthroughLabel ...string) {
	target := branchTarget{
		breakLabel:    breakLabel,
		continueLabel: continueLabel,
		userLabel:     strings.TrimSpace(userLabel),
	}
	if len(fallthroughLabel) > 0 {
		target.fallthroughLabel = fallthroughLabel[0]
	}
	l.pushBranchTarget(target)
}

func (l *lowerer) pushBranchTarget(target branchTarget) {
	l.branches = append(l.branches, target)
	if target.userLabel == "" {
		return
	}
	if l.labeledBranches == nil {
		l.labeledBranches = map[string][]branchTarget{}
	}
	l.labeledBranches[target.userLabel] = append(l.labeledBranches[target.userLabel], target)
}

func (l *lowerer) popBranch() {
	if len(l.branches) == 0 {
		return
	}
	target := l.branches[len(l.branches)-1]
	l.branches = l.branches[:len(l.branches)-1]
	if target.userLabel == "" {
		return
	}
	stack := l.labeledBranches[target.userLabel]
	if len(stack) <= 1 {
		delete(l.labeledBranches, target.userLabel)
		return
	}
	l.labeledBranches[target.userLabel] = stack[:len(stack)-1]
}

func (l *lowerer) lookupLabeledBranch(label string) (branchTarget, bool) {
	label = strings.TrimSpace(label)
	if label == "" {
		return branchTarget{}, false
	}
	stack := l.labeledBranches[label]
	if len(stack) == 0 {
		return branchTarget{}, false
	}
	return stack[len(stack)-1], true
}
