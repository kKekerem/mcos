# SSH ile girişte MCOS komutlarını göster (kullanıcının isteği: "ssh ile
# bağlanınca restart sw_adı, stop sw_adı gibi komutlar olsun").
#
# YALNIZCA etkileşimli bir uçbirimde: WinSCP'nin SCP kipi de kabuk açar ve
# oraya basılan her satır dosya aktarımını bozar; SFTP kabuk hiç açmaz.
if [ -n "$SSH_CONNECTION" ] && [ -t 0 ] && [ -t 1 ]; then
	echo
	echo "MCOS — sunucular:"
	liste 2>/dev/null
	echo
	echo "Komutlar: liste · baslat <ad> · durdur <ad> · yeniden <ad> · durum <ad> · konsol <ad> · komut <ad> <komut> · ekle <klasör> · yardim"
	echo "WinSCP: sunucular /data/sunucular altında; yeni sunucu klasörü atınca kendiliğinden eklenir."
	echo
fi
