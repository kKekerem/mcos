package daemon

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"mcos/internal/files"
	"mcos/internal/ipc"
	"mcos/internal/model"
	"mcos/internal/store"
)

// ── backup.policy / backup.setPolicy ────────────────────────────────────────
//
// Panel ve telefon uygulaması otomatik yedeğin aralığını, günün saatini ve
// saklanacak kopya sayısını buradan ayarlar. "Sonraki yedek" zamanı
// zamanlayıcının KENDİ kuralıyla (autoBackupFor) hesaplanır: arayüzde
// yazan saat ile yedeğin gerçekten alındığı saat ayrışamasın.

// backupNow is the clock of the policy handlers (testler değiştirir).
var backupNow = time.Now

func (d *Daemon) handleBackupPolicy(_ context.Context, raw json.RawMessage) (any, error) {
	var p ipc.BackupPolicyParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	srv, ierr := d.backupPolicyServer(p.ServerID)
	if ierr != nil {
		return nil, ierr
	}
	return d.backupPolicyResult(srv, backupNow()), nil
}

func (d *Daemon) handleBackupSetPolicy(_ context.Context, raw json.RawMessage) (any, error) {
	var p ipc.BackupSetPolicyParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	srv, ierr := d.backupPolicyServer(p.ServerID)
	if ierr != nil {
		return nil, ierr
	}
	pol, err := mergeBackupPolicy(srv.Backup, p)
	if err != nil {
		return nil, &ipc.Error{Code: ipc.CodeInvalidParams, Message: err.Error()}
	}
	srv.Backup = pol
	if err := d.store.SaveServer(srv); err != nil {
		return nil, err
	}
	d.log.Infof("backup: %s otomatik yedek planı: %s", srv.ID, model.BackupPolicyLabel(pol))
	return d.backupPolicyResult(srv, backupNow()), nil
}

// mergeBackupPolicy applies a setPolicy request to the stored policy.
//
// Doğrulama BURADA, kayıttan önce: geçersiz bir plan diske yazılsaydı
// zamanlayıcı onu her dakika okuyup atlayacak, kullanıcı ise panelde planın
// kaydedildiğini sanıp yedeksiz kalacaktı.
func mergeBackupPolicy(cur model.BackupPolicy, p ipc.BackupSetPolicyParams) (model.BackupPolicy, error) {
	pol := cur
	pol.Auto = p.Auto
	if s := strings.TrimSpace(p.Schedule); s != "" {
		sc, err := model.ParseBackupSchedule(s)
		if err != nil {
			return cur, err
		}
		// Kanonik biçimde saklanır ("1D@4:00" → "1d@04:00"): manifest ile
		// arayüzdeki "şu an seçili" işareti aynı dizgiyi karşılaştırsın.
		pol.Schedule = sc.String()
	}
	if p.Keep != nil {
		pol.Keep = *p.Keep
	}
	if pol.Auto && strings.TrimSpace(pol.Schedule) == "" {
		// Hiç ayarlanmamış bir planı yalnızca "aç" diyerek açmak: sihirbazın
		// varsayılanı. Keep yalnızca o da hiç verilmemişse varsayılana döner;
		// 0 (sınırsız) bilinçli bir seçim olabilir.
		pol.Schedule = model.DefaultBackupSchedule
		if p.Keep == nil && cur.Keep == 0 {
			pol.Keep = model.DefaultBackupKeep
		}
	}
	if err := model.ValidateBackupPolicy(pol); err != nil {
		return cur, err
	}
	return pol, nil
}

// backupPolicyServer loads the server of a policy request.
func (d *Daemon) backupPolicyServer(id string) (*model.Server, *ipc.Error) {
	if strings.TrimSpace(id) == "" {
		return nil, &ipc.Error{Code: ipc.CodeInvalidParams, Message: "sunucu kimliği (serverId) gerekli"}
	}
	if err := files.ValidServerID(id); err != nil {
		return nil, &ipc.Error{Code: ipc.CodeInvalidParams, Message: "geçersiz sunucu kimliği"}
	}
	srv, err := d.store.GetServer(id)
	if err != nil {
		if err == store.ErrNotFound {
			return nil, &ipc.Error{Code: ipc.CodeNotFound, Message: "sunucu bulunamadı"}
		}
		return nil, &ipc.Error{Code: ipc.CodeInternalError, Message: err.Error()}
	}
	return srv, nil
}

// backupPolicyResult describes the plan and the scheduler's next step.
func (d *Daemon) backupPolicyResult(srv *model.Server, now time.Time) ipc.BackupPolicyResult {
	loc := d.backupLocation()
	res := ipc.BackupPolicyResult{
		ServerID: srv.ID,
		Policy:   srv.Backup,
		Summary:  model.BackupPolicyLabel(srv.Backup),
	}
	if loc != time.Local {
		res.Timezone = loc.String()
	}
	if !srv.Backup.Auto {
		return res
	}
	list, err := d.backup.List(srv.ID)
	if err != nil {
		res.Problem = "yedek listesi okunamadı: " + err.Error()
		return res
	}
	v, problem := d.autoBackupFor(srv, list, now, loc)
	if problem != "" {
		res.Problem = problem
		return res
	}
	next := v.next.In(loc)
	res.Next = &next
	res.Due, res.Reason = v.due, v.reason
	res.Running = d.autoBackupBusy(srv.ID)
	if v.hasLast {
		last := v.last.In(loc)
		res.Last = &last
	}
	return res
}
