package panel

// Screenshot renders a single static frame of the panel for the given section
// at size w x h, using live data from the daemon. It is used for headless
// verification (no TTY) and debugging. section is a sidebar index; if it is
// secServers with no servers, the wizard is shown (mirroring the boot rule).
func Screenshot(cl *Client, themeName string, section, w, h int) string {
	a := NewApp(cl, themeName)
	a.width, a.height = w, h
	a.section = section
	a.focus = focusContent
	a.bootChecked = true

	if st, err := cl.Status(); err == nil {
		a.status = st
	}
	if s, err := cl.Servers(); err == nil {
		a.servers = s
	}
	if j, err := cl.JavaList(); err == nil {
		a.java = j
	}
	switch {
	case section <= setupSectionBase:
		// OOBE (ilk kurulum) sihirbazının belirli bir adımını göster.
		// Kodlama: section = setupSectionBase - adım.
		//
		// Bu yol OLMADAN kurulum ekranları headless olarak görüntülenemiyordu;
		// tasarım düzeltmeleri ancak ekranı görerek yapılabilir.
		step := setupSectionBase - section
		a.setup = newSetup(a.th, themeName)
		if cfg, err := cl.Config(); err == nil {
			a.config = cfg
		}
		if disks, err := cl.Disks(); err == nil {
			a.setup.disks = disks
			a.setup.disksLoading = false
		}
		a.setup.step = setupStep(step)
	case section < 0: // sentinel: show the install wizard frame
		a.openWizard()
	}
	return a.View()
}

// setupSectionBase, Screenshot()'ta OOBE adımlarını kodlamak için kullanılan
// taban değerdir: section = setupSectionBase - adım.
// -2 zaten "ekran görüntüsü modu kapalı" anlamına geldiği için (cmd/mcos-panel)
// yeterince uzak bir taban seçildi.
const setupSectionBase = -10
