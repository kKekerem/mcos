package daemon

import (
	"context"
	"encoding/json"
	"errors"

	"mcos/internal/cluster"
	"mcos/internal/ipc"
)

// Anahtarsız (kodla) eşleştirme uçları. Kullanıcı: "eşleştirme anahtarını
// elle girme gerekmesin, oto tarasın doğrulasın". Akışın tamamı
// internal/cluster/pairoffer.go'da; burası yalnızca panelin kapısı.

func (d *Daemon) handleClusterPairOffer(ctx context.Context, raw json.RawMessage) (any, error) {
	var p ipc.ClusterPairParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	code, err := d.cluster.PairOffer(ctx, p.ID)
	if errors.Is(err, cluster.ErrPairUnsupported) {
		return ipc.PairOfferResult{Supported: false}, nil
	}
	if err != nil {
		return nil, &ipc.Error{Code: ipc.CodeUnavailable, Message: err.Error()}
	}
	return ipc.PairOfferResult{Supported: true, Code: code}, nil
}

func (d *Daemon) handleClusterPairConfirm(ctx context.Context, raw json.RawMessage) (any, error) {
	var p ipc.ClusterPairParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	st, err := d.cluster.PairConfirm(ctx, p.ID)
	res := ipc.PairConfirmResult{State: st}
	if err != nil {
		res.Message = err.Error()
	}
	return res, nil
}

func (d *Daemon) handleClusterPairCancel(_ context.Context, raw json.RawMessage) (any, error) {
	var p ipc.ClusterPairParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	d.cluster.PairCancel(p.ID)
	return ipc.OKResult{OK: true}, nil
}
