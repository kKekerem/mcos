package server

import (
	"errors"
	"fmt"
)

// StartStage names the phase of Start() that failed. Kullanıcıya nerede
// takıldığını söylemek için kullanılır.
type StartStage string

const (
	StageInstall StartStage = "Kurulum"
	StageLaunch  StartStage = "Başlatma bilgisi"
	StageJava    StartStage = "Java"
	StageArgs    StartStage = "Başlatma parametreleri"
	StageSpawn   StartStage = "Süreç başlatma"
)

// StartError is a server-start failure with a user-facing Turkish explanation
// and the underlying technical cause kept for the log.
//
// Eskiden Start() ham Go hatalarını sarıp döndürüyordu ve panel bunları
// doğrudan gösteriyordu; kullanıcı
//
//	load launch info: open /data/servers/srv_x/data/.mcos-launch.json:
//	no such file or directory
//
// gibi bir metin görüyordu — ne olduğu ve ne yapması gerektiği belirsizdi.
type StartError struct {
	Stage StartStage // hangi aşamada
	Hint  string     // kullanıcı ne yapmalı
	Err   error      // teknik sebep
}

func (e *StartError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("%s: %s", e.Stage, e.Hint)
	}
	return fmt.Sprintf("%s: %s — teknik ayrıntı: %v", e.Stage, e.Hint, e.Err)
}

func (e *StartError) Unwrap() error { return e.Err }

// UserMessage returns just the human-facing part, without the technical cause.
// Panel bunu gösterir; teknik ayrıntı günlüğe yazılır.
func (e *StartError) UserMessage() string {
	return fmt.Sprintf("%s başarısız: %s", e.Stage, e.Hint)
}

// UserMessage extracts a Turkish, actionable message from any start error.
func UserMessage(err error) string {
	if err == nil {
		return ""
	}
	var se *StartError
	if errors.As(err, &se) {
		return se.UserMessage()
	}
	return err.Error()
}

func startErr(stage StartStage, hint string, err error) *StartError {
	return &StartError{Stage: stage, Hint: hint, Err: err}
}
