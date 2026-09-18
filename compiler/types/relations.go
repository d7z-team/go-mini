package types

type RelationCode string

const (
	RelationOK                        RelationCode = "relation.ok"
	RelationInvalid                   RelationCode = "relation.invalid"
	RelationNotIdentical              RelationCode = "relation.not_identical"
	RelationUnderlyingMismatch        RelationCode = "relation.underlying_mismatch"
	RelationBothTypesNamed            RelationCode = "relation.both_types_named"
	RelationNotAssignable             RelationCode = "relation.not_assignable"
	RelationInterfaceTypeSet          RelationCode = "relation.interface_type_set"
	RelationMissingMethod             RelationCode = "relation.method_missing"
	RelationMethodSignatureMismatch   RelationCode = "relation.method_signature"
	RelationSignatureArityMismatch    RelationCode = "relation.signature_arity"
	RelationSignatureVariadicMismatch RelationCode = "relation.signature_variadic"
	RelationNotNilable                RelationCode = "relation.not_nilable"
	RelationNotComparable             RelationCode = "relation.not_comparable"
	RelationNotStrictlyComparable     RelationCode = "relation.not_strictly_comparable"
	RelationNotOrdered                RelationCode = "relation.not_ordered"
)

type RelationResult struct {
	OK   bool
	Code RelationCode
}

func relationOK() RelationResult {
	return RelationResult{OK: true, Code: RelationOK}
}

func relationFail(code RelationCode) RelationResult {
	return RelationResult{Code: code}
}

type Relations struct {
	Table      *TypeTable
	ignoreTags bool
}

// UnderlyingIdenticalIgnoringTags compares underlying types for explicit Go
// conversions. Named components retain their identity.
func (r Relations) UnderlyingIdenticalIgnoringTags(left, right TypeRef) RelationResult {
	conversion := Relations{Table: r.Table, ignoreTags: true}
	return conversion.UnderlyingIdentical(left, right)
}

func NewRelations(table *TypeTable) Relations {
	return Relations{Table: table}
}

func (r Relations) View(ref TypeRef) TypeView {
	return View(r.Table, ref)
}

func (r Relations) Identical(left, right TypeRef) RelationResult {
	if !left.Valid() || !right.Valid() {
		return relationFail(RelationInvalid)
	}
	if r.identical(left, right, map[typeRefPair]bool{}) {
		return relationOK()
	}
	return relationFail(RelationNotIdentical)
}

func (r Relations) UnderlyingIdentical(left, right TypeRef) RelationResult {
	if !left.Valid() || !right.Valid() {
		return relationFail(RelationInvalid)
	}
	if r.identical(r.underlying(left), r.underlying(right), map[typeRefPair]bool{}) {
		return relationOK()
	}
	return relationFail(RelationUnderlyingMismatch)
}

// ResolveAlias returns the identity denoted by an alias declaration while
// preserving defined types.
func (r Relations) ResolveAlias(ref TypeRef) TypeRef {
	return r.aliasTarget(ref)
}

func (r Relations) Assignable(source, target TypeRef) RelationResult {
	if !source.Valid() || !target.Valid() {
		return relationFail(RelationInvalid)
	}
	if target.Kind == Any || r.Identical(source, target).OK {
		return relationOK()
	}
	if targetNode, ok := r.interfaceNode(target); ok && !targetNode.TypeSet {
		return r.Implements(source, target)
	}
	if _, ok := r.interfaceNode(source); ok {
		return relationFail(RelationNotAssignable)
	}
	if r.UnderlyingIdentical(source, target).OK {
		if !r.isNamed(source) || !r.isNamed(target) {
			return relationOK()
		}
		return relationFail(RelationBothTypesNamed)
	}
	sourceSig, sourceFunction := r.functionSignature(source)
	targetSig, targetFunction := r.functionSignature(target)
	if sourceFunction || targetFunction {
		if sourceFunction && targetFunction && r.SignatureIdentical(sourceSig, targetSig).OK {
			return relationOK()
		}
		return relationFail(RelationNotAssignable)
	}
	sourceWaitable, sourceOK := r.waitableInfo(source)
	targetWaitable, targetOK := r.waitableInfo(target)
	if sourceOK || targetOK {
		if sourceOK && targetOK &&
			sourceWaitable.Direction == ChannelBoth &&
			r.Identical(sourceWaitable.Elem, targetWaitable.Elem).OK &&
			(!r.isNamed(source) || !r.isNamed(target)) {
			return relationOK()
		}
		return relationFail(RelationNotAssignable)
	}
	return relationFail(RelationNotAssignable)
}

func (r Relations) Implements(source, target TypeRef) RelationResult {
	if !source.Valid() || !target.Valid() {
		return relationFail(RelationInvalid)
	}
	if target.Kind == Any {
		return relationOK()
	}
	targetNode, ok := r.interfaceNode(target)
	if !ok {
		return relationFail(RelationNotAssignable)
	}
	if targetNode.TypeSet {
		return relationFail(RelationInterfaceTypeSet)
	}
	sourceNode, sourceInterface := r.interfaceNode(source)
	if sourceInterface && sourceNode.TypeSet {
		return relationFail(RelationInterfaceTypeSet)
	}
	available := r.methodSet(source, sourceNode, sourceInterface)
	for _, method := range targetNode.Methods {
		candidate, exists := available[methodKey(method)]
		if !exists {
			return relationFail(RelationMissingMethod)
		}
		if !r.SignatureIdentical(candidate, method.Signature).OK {
			return relationFail(RelationMethodSignatureMismatch)
		}
	}
	return relationOK()
}

func (r Relations) SignatureIdentical(left, right FunctionSignature) RelationResult {
	if left.Variadic != right.Variadic {
		return relationFail(RelationSignatureVariadicMismatch)
	}
	if len(left.Params) != len(right.Params) || len(left.Results) != len(right.Results) {
		return relationFail(RelationSignatureArityMismatch)
	}
	for i := range left.Params {
		if !r.Identical(left.Params[i].Type, right.Params[i].Type).OK {
			return relationFail(RelationNotIdentical)
		}
	}
	for i := range left.Results {
		if !r.Identical(left.Results[i], right.Results[i]).OK {
			return relationFail(RelationNotIdentical)
		}
	}
	return relationOK()
}

func (r Relations) NilAssignable(target TypeRef) RelationResult {
	if r.View(target).Nilable() {
		return relationOK()
	}
	return relationFail(RelationNotNilable)
}

func (r Relations) Comparable(ref TypeRef) RelationResult {
	if r.View(ref).Comparable() {
		return relationOK()
	}
	return relationFail(RelationNotComparable)
}

func (r Relations) StrictlyComparable(ref TypeRef) RelationResult {
	if r.View(ref).StrictlyComparable() {
		return relationOK()
	}
	return relationFail(RelationNotStrictlyComparable)
}

func (r Relations) MapKeyAllowed(ref TypeRef) RelationResult {
	return r.Comparable(ref)
}

func (r Relations) Ordered(ref TypeRef) RelationResult {
	if r.View(ref).Ordered() {
		return relationOK()
	}
	return relationFail(RelationNotOrdered)
}

func (r Relations) identical(left, right TypeRef, seen map[typeRefPair]bool) bool {
	left = r.aliasTarget(left)
	right = r.aliasTarget(right)
	if left.Equal(right) {
		return true
	}
	if left.Kind != right.Kind {
		return false
	}
	pair := typeRefPair{Left: left, Right: right}
	if seen[pair] {
		return true
	}
	seen[pair] = true
	defer delete(seen, pair)
	if left.Kind == Named {
		return r.typeKey(left) == r.typeKey(right)
	}
	leftNode, leftOK := r.nodeForRef(left)
	rightNode, rightOK := r.nodeForRef(right)
	if leftOK || rightOK {
		return leftOK && rightOK && r.identicalNode(leftNode, rightNode, seen)
	}
	return left.Equal(right)
}

func (r Relations) identicalNode(left, right TypeNode, seen map[typeRefPair]bool) bool {
	if left.Kind != right.Kind {
		return false
	}
	switch left.Kind {
	case Slice, Pointer:
		return r.identical(left.Elem, right.Elem, seen)
	case Array:
		return left.Length >= 0 && right.Length >= 0 && left.Length == right.Length &&
			r.identical(left.Elem, right.Elem, seen)
	case Map:
		return r.identical(left.Key, right.Key, seen) && r.identical(left.Elem, right.Elem, seen)
	case Waitable:
		return left.Direction == right.Direction && r.identical(left.Elem, right.Elem, seen)
	case Function:
		if left.Signature == nil || right.Signature == nil {
			return left.Signature == nil && right.Signature == nil
		}
		return r.SignatureIdentical(*left.Signature, *right.Signature).OK
	case Tuple:
		if len(left.Tuple) != len(right.Tuple) {
			return false
		}
		for i := range left.Tuple {
			if !r.identical(left.Tuple[i], right.Tuple[i], seen) {
				return false
			}
		}
		return true
	case Struct:
		return r.identicalFields(left.Fields, right.Fields, seen)
	case Interface:
		return r.identicalInterface(left, right, seen)
	default:
		return false
	}
}

func (r Relations) identicalFields(left, right []Field, seen map[typeRefPair]bool) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i].Name != right[i].Name || !r.ignoreTags && left[i].Tag != right[i].Tag || left[i].Embedded != right[i].Embedded {
			return false
		}
		if !r.identical(left[i].Type, right[i].Type, seen) {
			return false
		}
	}
	return true
}

func (r Relations) identicalInterface(left, right TypeNode, seen map[typeRefPair]bool) bool {
	if left.TypeSet != right.TypeSet || len(left.Methods) != len(right.Methods) || len(left.Terms) != len(right.Terms) {
		return false
	}
	for i := range left.Methods {
		if methodKey(left.Methods[i]) != methodKey(right.Methods[i]) {
			return false
		}
		if !r.SignatureIdentical(left.Methods[i].Signature, right.Methods[i].Signature).OK {
			return false
		}
	}
	for i := range left.Terms {
		if left.Terms[i].Approx != right.Terms[i].Approx || left.Terms[i].Union != right.Terms[i].Union {
			return false
		}
		if !r.identical(left.Terms[i].Type, right.Terms[i].Type, seen) {
			return false
		}
	}
	return true
}

func (r Relations) aliasTarget(ref TypeRef) TypeRef {
	start := ref
	seen := map[TypeID]struct{}{}
	for {
		node, ok := r.nodeForRef(ref)
		if !ok || !node.Alias || !node.AliasTarget.Valid() {
			return ref
		}
		if _, duplicate := seen[node.ID]; duplicate {
			return start
		}
		seen[node.ID] = struct{}{}
		ref = node.AliasTarget
	}
}

func (r Relations) underlying(ref TypeRef) TypeRef {
	if r.Table == nil {
		return ref
	}
	return r.Table.Underlying(ref)
}

func (r Relations) isNamed(ref TypeRef) bool {
	return r.aliasTarget(ref).Kind == Named
}

func (r Relations) interfaceNode(ref TypeRef) (TypeNode, bool) {
	if r.Table == nil {
		return TypeNode{}, false
	}
	return r.Table.IsInterface(ref)
}

func (r Relations) functionSignature(ref TypeRef) (FunctionSignature, bool) {
	if r.Table == nil {
		return FunctionSignature{}, false
	}
	return r.Table.IsFunction(ref)
}

func (r Relations) waitableInfo(ref TypeRef) (waitableInfo, bool) {
	ref = r.underlying(ref)
	if ref.Kind != Waitable || ref.Node == "" {
		return waitableInfo{}, false
	}
	node, ok := r.nodeForRef(ref)
	if !ok {
		return waitableInfo{}, false
	}
	return waitableInfo{Direction: node.Direction, Elem: node.Elem}, true
}

func (r Relations) methodSet(source TypeRef, sourceNode TypeNode, sourceInterface bool) map[string]FunctionSignature {
	available := map[string]FunctionSignature{}
	if sourceInterface {
		for _, method := range sourceNode.Methods {
			available[methodKey(method)] = method.Signature
		}
		return available
	}
	type frontierItem struct {
		typ     TypeRef
		pointer bool
		seen    map[TypeRef]struct{}
	}
	base, pointer := r.methodSetBase(source)
	frontier := []frontierItem{{typ: base, pointer: pointer, seen: map[TypeRef]struct{}{base: {}}}}
	for depth := 0; len(frontier) != 0; depth++ {
		candidates := map[string]FunctionSignature{}
		counts := map[string]int{}
		next := []frontierItem{}
		for _, item := range frontier {
			for _, method := range r.directMethodSet(item.typ, item.pointer) {
				key := methodKey(method)
				if _, exists := available[key]; exists {
					continue
				}
				candidates[key] = method.Signature
				counts[key]++
			}
			for _, field := range r.embeddedMethodSetFields(item.typ) {
				fieldBase, fieldPointer := r.methodSetBase(field.Type)
				fieldPointer = fieldPointer || item.pointer
				if _, recursive := item.seen[fieldBase]; recursive {
					continue
				}
				seen := make(map[TypeRef]struct{}, len(item.seen)+1)
				for typ := range item.seen {
					seen[typ] = struct{}{}
				}
				seen[fieldBase] = struct{}{}
				next = append(next, frontierItem{typ: fieldBase, pointer: fieldPointer, seen: seen})
			}
		}
		for key, signature := range candidates {
			if counts[key] == 1 {
				available[key] = signature
			}
		}
		if depth == 0 {
			// Direct methods always hide promoted methods with the same identity.
			for key := range candidates {
				if counts[key] > 1 {
					delete(available, key)
				}
			}
		}
		frontier = next
	}
	return available
}

func (r Relations) methodSetBase(ref TypeRef) (TypeRef, bool) {
	ref = r.aliasTarget(ref)
	pointer := r.View(ref).Shape() == Pointer
	if pointer {
		if elem, ok := r.View(ref).Elem(); ok {
			ref = r.aliasTarget(elem)
		}
	}
	return ref, pointer
}

func (r Relations) directMethodSet(ref TypeRef, pointer bool) []Method {
	ref = r.aliasTarget(ref)
	node, ok := r.nodeForRef(ref)
	if !ok {
		return nil
	}
	out := make([]Method, 0, len(node.Methods))
	for _, method := range node.Methods {
		if method.Receiver.Valid() && !pointer && r.View(method.Receiver).Shape() == Pointer {
			continue
		}
		out = append(out, method)
	}
	return out
}

func (r Relations) embeddedMethodSetFields(ref TypeRef) []Field {
	ref = r.aliasTarget(ref)
	fields, ok := r.View(ref).StructFields()
	if !ok {
		return nil
	}
	out := make([]Field, 0, len(fields))
	for _, field := range fields {
		if field.Embedded {
			out = append(out, field)
		}
	}
	return out
}

func (r Relations) nodeForRef(ref TypeRef) (TypeNode, bool) {
	if r.Table == nil {
		return TypeNode{}, false
	}
	return r.Table.nodeForRef(ref)
}

func (r Relations) typeKey(ref TypeRef) TypeKey {
	if node, ok := r.nodeForRef(ref); ok && !emptyTypeKey(node.Identity) {
		return node.Identity
	}
	return ref.Named
}

type typeRefPair struct {
	Left  TypeRef
	Right TypeRef
}
