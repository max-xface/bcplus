// Code generated by "stringer -type MatKind"; DO NOT EDIT.

package cmdr

import "strconv"

const _MatKind_name = "MatRawMatManMatEnc"

var _MatKind_index = [...]uint8{0, 6, 12, 18}

func (i MatKind) String() string {
	if i < 0 || i >= MatKind(len(_MatKind_index)-1) {
		return "MatKind(" + strconv.FormatInt(int64(i), 10) + ")"
	}
	return _MatKind_name[_MatKind_index[i]:_MatKind_index[i+1]]
}