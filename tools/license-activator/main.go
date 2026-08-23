package main

import (
	"crypto/sha256"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"net"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

const secretKey = "thingue-launcher-license-secret-v1"

var baseTime = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

func main() {
	a := app.New()
	w := a.NewWindow("ThingUE License Activator")
	w.Resize(fyne.NewSize(680, 520))

	// ── 申请码区 ──────────────────────────────────────────
	appCodeEntry := widget.NewEntry()
	appCodeEntry.PlaceHolder = "请输入申请码"
	appCodeEntry.SetText(generateApplicationCode())

	fillBtn := widget.NewButton("使用本机申请码", func() {
		appCodeEntry.SetText(generateApplicationCode())
	})

	appCodeCard := widget.NewCard("申请码", "",
		container.NewBorder(nil, nil, nil, fillBtn, appCodeEntry),
	)

	// ── 授权时长区 ────────────────────────────────────────
	durationSelect := widget.NewSelect([]string{"日", "周", "月", "年", "永久"}, func(string) {})
	durationSelect.Selected = "月"

	countEntry := widget.NewEntry()
	countEntry.PlaceHolder = "数量（例如：1、30）"
	countEntry.SetText("1")

	durationCard := widget.NewCard("授权时长", "",
		container.NewBorder(nil, nil, durationSelect, nil, countEntry),
	)

	// ── 激活码展示区（初始隐藏）────────────────────────────
	var generatedLicense string

	licenseEntry := widget.NewEntry()
	licenseEntry.MultiLine = true
	licenseEntry.Wrapping = fyne.TextWrapBreak
	licenseEntry.Disable()

	copyBtn := widget.NewButton("复制激活码", func() {
		if generatedLicense == "" {
			return
		}
		w.Clipboard().SetContent(generatedLicense)
		dialog.ShowInformation("已复制", "激活码已复制到剪贴板", w)
	})

	saveBtn := widget.NewButton("保存文件", func() {
		if generatedLicense == "" {
			return
		}
		fd := dialog.NewFileSave(func(uc fyne.URIWriteCloser, err error) {
			if err != nil || uc == nil {
				return
			}
			defer uc.Close()
			if _, werr := uc.Write([]byte(generatedLicense)); werr != nil {
				dialog.ShowError(werr, w)
				return
			}
			dialog.ShowInformation("保存成功", "激活文件已保存", w)
		}, w)
		fd.SetFileName("thingue.lic")
		fd.Show()
	})

	btnRow := container.New(layout.NewGridLayout(2), copyBtn, saveBtn)
	licenseCard := widget.NewCard("激活码", "", container.NewVBox(licenseEntry, btnRow))
	licenseCard.Hide()

	// ── 生成按钮 ──────────────────────────────────────────
	generateBtn := widget.NewButton("生成激活码", func() {
		ac := strings.TrimSpace(appCodeEntry.Text)
		if ac == "" {
			dialog.ShowError(fmt.Errorf("请输入申请码"), w)
			return
		}
		sel := strings.TrimSpace(durationSelect.Selected)
		if sel == "" {
			dialog.ShowError(fmt.Errorf("请选择授权时长"), w)
			return
		}

		issued := time.Now().UTC()
		var exp time.Time
		var qty int

		if sel != "永久" {
			q := strings.TrimSpace(countEntry.Text)
			if q == "" {
				dialog.ShowError(fmt.Errorf("请输入数量"), w)
				return
			}
			n, err := strconv.Atoi(q)
			if err != nil || n <= 0 {
				dialog.ShowError(fmt.Errorf("数量格式错误"), w)
				return
			}
			qty = n
		}

		switch sel {
		case "日":
			exp = issued.AddDate(0, 0, qty)
		case "周":
			exp = issued.AddDate(0, 0, 7*qty)
		case "月":
			exp = issued.AddDate(0, qty, 0)
		case "年":
			exp = issued.AddDate(qty, 0, 0)
		case "永久":
			exp = issued.AddDate(100, 0, 0)
		}

		content, err := encryptHexLicense(ac, exp.Unix())
		if err != nil {
			dialog.ShowError(err, w)
			return
		}

		generatedLicense = content
		licenseEntry.SetText(content)
		licenseCard.Show()
		licenseCard.Refresh()
		w.Content().Refresh()
	})

	// ── 总布局 ────────────────────────────────────────────
	w.SetContent(container.NewPadded(
		container.NewVBox(
			appCodeCard,
			durationCard,
			container.New(layout.NewGridLayout(1), generateBtn),
			licenseCard,
		),
	))

	w.ShowAndRun()
}

func generateApplicationCode() string {
	fp := generateFingerprint()
	h := sha256.Sum256([]byte(fp))
	enc := base32.StdEncoding.WithPadding(base32.NoPadding)
	return enc.EncodeToString(h[:])
}

func generateFingerprint() string {
	host, _ := os.Hostname()
	host = strings.TrimSpace(strings.ToLower(host))

	mac := "NOMAC"
	ifaces, _ := net.Interfaces()
	sort.SliceStable(ifaces, func(i, j int) bool { return strings.ToLower(ifaces[i].Name) < strings.ToLower(ifaces[j].Name) })
	for _, it := range ifaces {
		if (it.Flags&net.FlagLoopback) == 0 && len(it.HardwareAddr) > 0 {
			mac = strings.ToUpper(it.HardwareAddr.String())
			break
		}
	}
	return fmt.Sprintf("%s|%s|%s|%s", host, runtime.GOOS, runtime.GOARCH, mac)
}

func encryptHexLicense(appCode string, expiresAt int64) (string, error) {
	enc := base32.StdEncoding.WithPadding(base32.NoPadding)
	appCodeBytes, err := enc.DecodeString(strings.ToUpper(strings.TrimSpace(appCode)))
	if err != nil {
		return "", fmt.Errorf("invalid application code: %v", err)
	}
	if len(appCodeBytes) < 4 {
		return "", fmt.Errorf("application code too short")
	}
	machineID := binary.BigEndian.Uint32(appCodeBytes[:4]) & 0xFFFFFF

	expTime := time.Unix(expiresAt, 0).UTC()
	hours := uint32(expTime.Sub(baseTime).Hours())
	if hours > 0xFFFFFF {
		return "", fmt.Errorf("expiration date too far in future (max ~1900 years)")
	}

	verifyBuf := make([]byte, 8)
	binary.BigEndian.PutUint32(verifyBuf[0:4], hours)
	binary.BigEndian.PutUint32(verifyBuf[4:8], machineID)
	checksum := crc32.ChecksumIEEE(verifyBuf) & 0xFFFF

	var payload uint64
	payload |= uint64(hours) << 40
	payload |= uint64(machineID) << 16
	payload |= uint64(checksum)

	keyHash := sha256.Sum256([]byte(secretKey))
	mask := binary.BigEndian.Uint64(keyHash[:8])
	cipherVal := payload ^ mask

	rawStr := fmt.Sprintf("%016X", cipherVal)

	var sb strings.Builder
	for i, r := range rawStr {
		if i > 0 && i%4 == 0 {
			sb.WriteRune('-')
		}
		sb.WriteRune(r)
	}
	return sb.String(), nil
}
