package gateway

import (
	"errors"
	"sort"

	"github.com/d7z-team/mini-go/rpc"
	rpcrouter "github.com/d7z-team/mini-go/rpc/router"
)

func contractReferenceToWire(reference rpcrouter.ContractReference) MiniGoGatewayControlContractReference {
	return MiniGoGatewayControlContractReference{ImportPath: reference.ImportPath, Hash: reference.Hash}
}

func contractReferenceFromWire(reference MiniGoGatewayControlContractReference) rpcrouter.ContractReference {
	return rpcrouter.ContractReference{ImportPath: reference.ImportPath, Hash: reference.Hash}
}

func contractBundleToWire(bundle rpcrouter.MRPCBundle) MiniGoGatewayControlContractBundle {
	files := make([]MiniGoGatewayControlContractFile, len(bundle.Files))
	for index, file := range bundle.Files {
		files[index] = MiniGoGatewayControlContractFile{Path: file.Path, Text: file.Text, Hash: file.Hash}
	}
	return MiniGoGatewayControlContractBundle{ImportPath: bundle.ImportPath, Hash: bundle.Hash, Files: files}
}

func contractBundleFromWire(bundle MiniGoGatewayControlContractBundle) rpcrouter.MRPCBundle {
	files := make([]rpcrouter.MRPCFile, len(bundle.Files))
	for index, file := range bundle.Files {
		files[index] = rpcrouter.MRPCFile{Path: file.Path, Text: file.Text, Hash: file.Hash}
	}
	return rpcrouter.MRPCBundle{ImportPath: bundle.ImportPath, Hash: bundle.Hash, Files: files}
}

func catalogSnapshotToWire(snapshot Snapshot) MiniGoGatewayControlCatalogSnapshot {
	references := make([]MiniGoGatewayControlContractReference, len(snapshot.References))
	for index, reference := range snapshot.References {
		references[index] = contractReferenceToWire(reference)
	}
	return MiniGoGatewayControlCatalogSnapshot{
		Protocol: snapshot.Protocol, ID: snapshot.ID, GatewayID: snapshot.GatewayID,
		RouteEpoch: snapshot.RouteEpoch, References: references,
	}
}

func catalogSnapshotFromWire(snapshot MiniGoGatewayControlCatalogSnapshot) Snapshot {
	references := make([]rpcrouter.ContractReference, len(snapshot.References))
	for index, reference := range snapshot.References {
		references[index] = contractReferenceFromWire(reference)
	}
	return Snapshot{
		Protocol: snapshot.Protocol, ID: snapshot.ID, GatewayID: snapshot.GatewayID,
		RouteEpoch: snapshot.RouteEpoch, References: references,
	}
}

func contractToWire(contract rpc.Contract) MiniGoGatewayControlContract {
	methods := make([]MiniGoGatewayControlMethod, len(contract.Methods))
	for index, method := range contract.Methods {
		methods[index] = MiniGoGatewayControlMethod{
			ID: method.ID, Service: method.Service, Name: method.Name,
			ContractHash: method.ContractHash, ResourceTypeHash: method.ResourceTypeHash,
		}
	}
	return MiniGoGatewayControlContract{Protocol: contract.Protocol, Methods: methods}
}

func contractFromWire(contract MiniGoGatewayControlContract) rpc.Contract {
	methods := make([]rpc.Method, len(contract.Methods))
	for index, method := range contract.Methods {
		methods[index] = rpc.Method{
			ID: method.ID, Service: method.Service, Name: method.Name,
			ContractHash: method.ContractHash, ResourceTypeHash: method.ResourceTypeHash,
		}
	}
	return rpc.Contract{Protocol: contract.Protocol, Methods: methods}
}

func registrationOptionsToWire(options rpcrouter.RegistrationOptions) MiniGoGatewayControlRegistrationOptions {
	names := make([]string, 0, len(options.Labels))
	for name := range options.Labels {
		names = append(names, name)
	}
	sort.Strings(names)
	labels := make([]MiniGoGatewayControlRouteLabel, len(names))
	for index, name := range names {
		labels[index] = MiniGoGatewayControlRouteLabel{Name: name, Value: options.Labels[name]}
	}
	return MiniGoGatewayControlRegistrationOptions{
		Name: options.Name, Priority: int64(options.Priority), Weight: int64(options.Weight),
		MaxLeases: int64(options.MaxLeases), Labels: labels,
	}
}

func registrationOptionsFromWire(options MiniGoGatewayControlRegistrationOptions) (rpcrouter.RegistrationOptions, error) {
	priority, weight, maxLeases := int(options.Priority), int(options.Weight), int(options.MaxLeases)
	if int64(priority) != options.Priority || int64(weight) != options.Weight || int64(maxLeases) != options.MaxLeases {
		return rpcrouter.RegistrationOptions{}, errors.New("Gateway publication provider options overflow host int")
	}
	if len(options.Labels) > maxProviderLabels {
		return rpcrouter.RegistrationOptions{}, errors.New("Gateway publication provider contains too many labels")
	}
	var labels map[string]string
	if len(options.Labels) != 0 {
		labels = make(map[string]string, len(options.Labels))
	}
	for _, label := range options.Labels {
		if _, duplicate := labels[label.Name]; duplicate {
			return rpcrouter.RegistrationOptions{}, errors.New("Gateway publication provider contains duplicate labels")
		}
		labels[label.Name] = label.Value
	}
	return rpcrouter.RegistrationOptions{
		Name: options.Name, Priority: priority, Weight: weight,
		MaxLeases: maxLeases, Labels: labels,
	}, nil
}

func publicationSnapshotToWire(snapshot PublicationSnapshot) MiniGoGatewayControlPublicationSnapshot {
	providers := make([]MiniGoGatewayControlPublishedProvider, len(snapshot.Providers))
	for index, provider := range snapshot.Providers {
		references := make([]MiniGoGatewayControlContractReference, len(provider.References))
		for referenceIndex, reference := range provider.References {
			references[referenceIndex] = contractReferenceToWire(reference)
		}
		providers[index] = MiniGoGatewayControlPublishedProvider{
			ID: provider.ID, Contract: contractToWire(provider.Contract),
			Options: registrationOptionsToWire(provider.Options), References: references,
		}
	}
	return MiniGoGatewayControlPublicationSnapshot{
		Protocol: snapshot.Protocol, ProcessID: snapshot.ProcessID,
		Generation: snapshot.Generation, ID: snapshot.ID, Providers: providers,
	}
}

func publicationSnapshotFromWire(snapshot MiniGoGatewayControlPublicationSnapshot) (PublicationSnapshot, error) {
	if len(snapshot.Providers) > maxPublishedProviders {
		return PublicationSnapshot{}, errors.New("Gateway publication contains too many providers")
	}
	providers := make([]PublishedProvider, len(snapshot.Providers))
	for index, provider := range snapshot.Providers {
		if len(provider.References) > maxProviderReferences {
			return PublicationSnapshot{}, errors.New("Gateway publication provider contains too many contract references")
		}
		references := make([]rpcrouter.ContractReference, len(provider.References))
		for referenceIndex, reference := range provider.References {
			references[referenceIndex] = contractReferenceFromWire(reference)
		}
		options, err := registrationOptionsFromWire(provider.Options)
		if err != nil {
			return PublicationSnapshot{}, err
		}
		providers[index] = PublishedProvider{
			ID: provider.ID, Contract: contractFromWire(provider.Contract),
			Options: options, References: references,
		}
	}
	return PublicationSnapshot{
		Protocol: snapshot.Protocol, ProcessID: snapshot.ProcessID,
		Generation: snapshot.Generation, ID: snapshot.ID, Providers: providers,
	}, nil
}
