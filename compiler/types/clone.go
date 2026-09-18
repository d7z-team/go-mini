package types

// CloneTable returns an owned type table without copying transient indexes.
func CloneTable(table TypeTable) TypeTable {
	out := TypeTable{Nodes: make([]TypeNode, len(table.Nodes))}
	for index, node := range table.Nodes {
		out.Nodes[index] = node
		out.Nodes[index].Tuple = append([]TypeRef(nil), node.Tuple...)
		out.Nodes[index].Fields = append([]Field(nil), node.Fields...)
		out.Nodes[index].Methods = make([]Method, len(node.Methods))
		for methodIndex, method := range node.Methods {
			out.Nodes[index].Methods[methodIndex] = method
			out.Nodes[index].Methods[methodIndex].Signature = cloneFunctionSignature(method.Signature)
		}
		out.Nodes[index].Terms = append([]TypeTerm(nil), node.Terms...)
		out.Nodes[index].TypeArgs = append([]TypeRef(nil), node.TypeArgs...)
		if node.Signature != nil {
			signature := cloneFunctionSignature(*node.Signature)
			out.Nodes[index].Signature = &signature
		}
	}
	return out
}

func cloneFunctionSignature(signature FunctionSignature) FunctionSignature {
	signature.Params = append([]TypeParam(nil), signature.Params...)
	signature.Results = append([]TypeRef(nil), signature.Results...)
	return signature
}
