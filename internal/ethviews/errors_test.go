package ethviews

import (
	"errors"

	"github.com/allinbits/ibc-monitoring/internal/ethrpc"
)

func asRevert(err error, target **ethrpc.ErrRevert) bool { return errors.As(err, target) }
