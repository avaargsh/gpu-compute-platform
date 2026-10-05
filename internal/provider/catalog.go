package provider

const KueueAdapterName = "kueue"

func IsSupportedAdapter(name string) bool {
	switch name {
	case KueueAdapterName:
		return true
	default:
		return false
	}
}

func SupportedAdapterNames() []string {
	return []string{KueueAdapterName}
}
