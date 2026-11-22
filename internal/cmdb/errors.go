package cmdb

import (
	"errors"
	"fmt"
)

// ErrCINotFound 表示指定 CI 不存在或当前租户不可见，供 HTTP 层稳定映射 404。
var ErrCINotFound = errors.New("CI not found")

func ciNotFound(id string) error {
	if id == "" {
		return ErrCINotFound
	}
	return fmt.Errorf("%w: %s", ErrCINotFound, id)
}
