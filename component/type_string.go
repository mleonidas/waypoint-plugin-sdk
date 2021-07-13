// Code generated by "stringer -type=Type -linecomment"; DO NOT EDIT.

package component

import "strconv"

func _() {
	// An "invalid array index" compiler error signifies that the constant values have changed.
	// Re-run the stringer command to generate them again.
	var x [1]struct{}
	_ = x[InvalidType-0]
	_ = x[BuilderType-1]
	_ = x[RegistryType-2]
	_ = x[PlatformType-3]
	_ = x[ReleaseManagerType-4]
	_ = x[LogPlatformType-5]
	_ = x[AuthenticatorType-6]
	_ = x[MapperType-7]
	_ = x[ConfigSourcerType-8]
	_ = x[TaskLauncherType-9]
	_ = x[maxType-10]
}

const _Type_name = "InvalidBuilderRegistryPlatformReleaseManagerLogPlatformAuthenticatorMapperConfigSourcerTaskLaunchermaxType"

var _Type_index = [...]uint8{0, 7, 14, 22, 30, 44, 55, 68, 74, 87, 99, 106}

func (i Type) String() string {
	if i >= Type(len(_Type_index)-1) {
		return "Type(" + strconv.FormatInt(int64(i), 10) + ")"
	}
	return _Type_name[_Type_index[i]:_Type_index[i+1]]
}
