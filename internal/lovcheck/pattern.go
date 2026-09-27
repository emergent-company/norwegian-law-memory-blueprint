package lovcheck

import "regexp"

// refIDPattern matches a valid Law/Regulation ref_id: lov/ or forskrift/
// followed by YYYY-MM-DD and an optional trailing numeric suffix. Historic acts
// legitimately omit the suffix (e.g. lov/1687-04-15), hence `(-\d+)?`.
var refIDPattern = regexp.MustCompile(`^(lov|forskrift)/\d{4}-\d{2}-\d{2}(-\d+)?$`)
