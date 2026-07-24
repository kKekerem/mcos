package main
import (
	"fmt"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)
func main() {
	lipgloss.SetColorProfile(termenv.TrueColor)
	style := lipgloss.NewStyle().Foreground(lipgloss.Color("#EEEEEE")).Background(lipgloss.Color("#1A1A1A"))
	fmt.Printf("%q\n", style.Render("TEST"))
}
