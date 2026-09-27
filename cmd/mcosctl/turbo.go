package main

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"mcos/internal/ipc"
	"mcos/internal/model"
)

// cmdTurbo: "mcosctl turbo [ac|kapat|durum]".
//
// Neden var: turbonun donanımda gerçekten ne yaptığı yalnızca panelde
// görünüyordu; seri konsoldan ya da SSH'tan doğrulamanın yolu yoktu. Madde
// madde çıktı, "desteklenmiyor" durumlarını da açıkça gösterir.
func cmdTurbo(cli *ipc.Client, args []string) {
	sub := "durum"
	if len(args) > 0 {
		sub = strings.ToLower(args[0])
	}
	var st *model.TurboStatus
	switch sub {
	case "ac", "aç", "on":
		st = turboSet(cli, true)
	case "kapat", "off":
		st = turboSet(cli, false)
	case "durum", "status":
		var s model.SystemStatus
		if err := cli.Call(ipc.MethodSystemStatus, nil, &s); err != nil {
			fail("turbo durum: %v", err)
		}
		st = s.Turbo
		if len(s.CPU.CoreMHz) > 0 {
			defer fmt.Printf("Çekirdek MHz:  %v\n", s.CPU.CoreMHz)
		}
	default:
		fail("usage: mcosctl turbo [ac|kapat|durum]")
	}
	if st == nil {
		fmt.Println("turbo ayrıntısı yok (eski daemon?)")
		return
	}
	printTurbo(st)
}

func turboSet(cli *ipc.Client, on bool) *model.TurboStatus {
	var res ipc.TurboResult
	if err := cli.Call(ipc.MethodSystemTurbo, ipc.TurboParams{Enabled: on}, &res); err != nil {
		fail("turbo: %v", err)
	}
	return res.Detail
}

func printTurbo(st *model.TurboStatus) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "Turbo:\t%s\n", st.Summary)
	fmt.Fprintf(w, "Etkin:\t%v\tDonanım desteği:\t%v\n", st.Active, st.Supported)
	if st.Driver != "" {
		fmt.Fprintf(w, "Sürücü:\t%s\tYönetici:\t%s\n", st.Driver, st.Governor)
	}
	fmt.Fprintf(w, "P-çekirdekleri:\t%s\tE-çekirdekleri:\t%s\t(%s)\n", st.PCores, dash(st.ECores), st.CoreMethod)
	if st.MaxMHz > 0 {
		fmt.Fprintf(w, "Frekans:\ttemel %d MHz, azami %d MHz, şu an %d MHz\n", st.BaseMHz, st.MaxMHz, st.CurMHz)
	}
	fmt.Fprintf(w, "Fanlar:\t%d/%d tam güçte\tSabitlenen iş parçacığı:\t%d\n", st.FansFull, st.FansTotal, st.PinnedThreads)
	w.Flush()
	for _, it := range st.Items {
		fmt.Printf("  [%s] %s: %s\n", it.State, it.Name, it.Detail)
	}
	// Tanı: donanımdan geri okunan durum ve ölçüm (panelde "d" tuşu,
	// /data/log/turbo.log). "Neden 4,4 değil 2,4 GHz" sorusunun cevabı.
	if st.Limiter != "" {
		fmt.Printf("Sınırlayan:  %s\n", st.Limiter)
	}
	if len(st.Diag) > 0 {
		fmt.Println("  --- tanı ---")
	}
	for _, it := range st.Diag {
		fmt.Printf("  [%s] %s: %s\n", it.State, it.Name, it.Detail)
	}
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
