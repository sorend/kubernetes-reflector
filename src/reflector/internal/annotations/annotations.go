package annotations

const Prefix = "reflector.v2.sorend.github.com"

const (
	ReflectionAllowed           = Prefix + "/reflection-allowed"
	ReflectionAllowedNamespaces = Prefix + "/reflection-allowed-namespaces"
	ReflectionAllowedNsSelector = Prefix + "/reflection-allowed-namespaces-selector"
	ReflectionAutoEnabled       = Prefix + "/reflection-auto-enabled"
	ReflectionAutoNamespaces    = Prefix + "/reflection-auto-namespaces"
	ReflectionAutoNsSelector    = Prefix + "/reflection-auto-namespaces-selector"
	Reflects                    = Prefix + "/reflects"
	MetaAutoReflects            = Prefix + "/auto-reflects"
	MetaReflectedVersion        = Prefix + "/reflected-version"
	MetaReflectedAt             = Prefix + "/reflected-at"
)
