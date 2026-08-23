package service

import (
	"crypto/sha256"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"net"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

// 授权状态
type LicenseStatus struct {
	Valid       bool
	Reason      string
	ExpiresAt   time.Time
	Fingerprint string
}

// 注意：默认授权文件路径可通过环境变量覆盖
// 优先级：THINGUE_LICENSE_PATH > X_LICENSE_PATH > 默认路径
const defaultLicensePath = "./thingue-launcher/thingue.lic"

// 简化：取消公钥要求
const defaultPublicKeyBase64 = ""
const secretKey = "thingue-launcher-license-secret-v1"

// Base Time for license expiration (2024-01-01 00:00:00 UTC)
var baseTime = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

type licenseService struct{}

var LicenseService = &licenseService{}

// 生成当前机器的申请码（Base32 无填充）
func (s *licenseService) GenerateApplicationCode() string {
	fp := s.generateFingerprint()
	h := sha256.Sum256([]byte(fp))
	enc := base32.StdEncoding.WithPadding(base32.NoPadding)
	return enc.EncodeToString(h[:])
}

// 对外方法：确保已授权，未授权或过期返回错误
func (s *licenseService) EnsureAuthorized() error {
	status, err := s.LoadAndVerify(s.LicensePath())
	if err != nil {
		return err
	}
	if !status.Valid {
		if status.Reason != "" {
			return errors.New(status.Reason)
		}
		return errors.New("未授权")
	}
	return nil
}

// 读取并校验授权文件
func (s *licenseService) LoadAndVerify(path string) (LicenseStatus, error) {
	st := LicenseStatus{Valid: false}
	appCode := s.GenerateApplicationCode()

	b, err := os.ReadFile(path)
	if err != nil {
		st.Reason = fmt.Sprintf("未授权：未检测到激活文件（%s），申请码：%s", path, appCode)
		return st, errors.New(st.Reason)
	}

	// 1. 解析激活码 (16位十六进制)
	// Payload Layout (64 bits): [ExpiresHours 24] [MachineID 24] [Checksum 16]
	decryptedVal, err := s.decryptHexLicense(string(b))
	if err != nil {
		st.Reason = fmt.Sprintf("未授权：激活码格式无效，申请码：%s", appCode)
		return st, errors.New(st.Reason)
	}

	// Unpack Fields
	checksum := uint16(decryptedVal & 0xFFFF)
	machineID := uint32((decryptedVal >> 16) & 0xFFFFFF)
	hours := uint32((decryptedVal >> 40) & 0xFFFFFF)

	// 2. 校验完整性 (Checksum)
	// Re-calculate Checksum: CRC32(Hours || MachineID) & 0xFFFF
	verifyBuf := make([]byte, 8)
	binary.BigEndian.PutUint32(verifyBuf[0:4], hours)
	binary.BigEndian.PutUint32(verifyBuf[4:8], machineID)
	calcedCrc := crc32.ChecksumIEEE(verifyBuf) & 0xFFFF

	if uint16(calcedCrc) != checksum {
		st.Reason = fmt.Sprintf("未授权：激活码校验失败，申请码：%s", appCode)
		return st, errors.New(st.Reason)
	}

	// 3. 校验设备 (MachineID)
	// MachineID = First 3 bytes of SHA256(Fingerprint)
	fp := s.generateFingerprint()
	fpHash := sha256.Sum256([]byte(fp))
	localMachineID := binary.BigEndian.Uint32(fpHash[:4]) & 0xFFFFFF

	if machineID != localMachineID {
		st.Reason = fmt.Sprintf("未授权：激活码不匹配当前设备，申请码：%s", appCode)
		// 记录指纹以便调试
		st.Fingerprint = fmt.Sprintf("Required: %x, Actual: %x", machineID, localMachineID)
		return st, errors.New(st.Reason)
	}
	st.Fingerprint = fp

	// 4. 校验过期时间
	now := time.Now().UTC()
	exp := baseTime.Add(time.Duration(hours) * time.Hour)
	st.ExpiresAt = exp
	if now.After(exp) {
		st.Reason = fmt.Sprintf("授权已过期，申请码：%s", appCode)
		return st, errors.New(st.Reason)
	}

	st.Valid = true
	return st, nil
}

// 计算设备指纹：Hostname | GOOS | GOARCH | Primary MAC（非回环）
func (s *licenseService) generateFingerprint() string {
	host, _ := os.Hostname()
	host = strings.TrimSpace(strings.ToLower(host))

	mac := "NOMAC"
	ifaces, _ := net.Interfaces()
	// 为稳定性，对接口按名称排序后选择
	sort.SliceStable(ifaces, func(i, j int) bool { return strings.ToLower(ifaces[i].Name) < strings.ToLower(ifaces[j].Name) })
	for _, it := range ifaces {
		// 过滤回环与无 MAC
		if (it.Flags&net.FlagLoopback) == 0 && len(it.HardwareAddr) > 0 {
			mac = strings.ToUpper(it.HardwareAddr.String())
			break
		}
	}
	return fmt.Sprintf("%s|%s|%s|%s", host, runtime.GOOS, runtime.GOARCH, mac)
}

// 激活文件路径解析
func (s *licenseService) LicensePath() string {
	if p := strings.TrimSpace(os.Getenv("THINGUE_LICENSE_PATH")); p != "" {
		return p
	}
	if p := strings.TrimSpace(os.Getenv("X_LICENSE_PATH")); p != "" {
		return p
	}
	return defaultLicensePath
}

func (s *licenseService) decryptHexLicense(encryptedStr string) (uint64, error) {
	// 1. 移除格式化字符
	cleanStr := strings.ReplaceAll(encryptedStr, "-", "")
	cleanStr = strings.ReplaceAll(cleanStr, " ", "")
	cleanStr = strings.TrimSpace(cleanStr)

	// 2. Parse Hex (Base 16)
	val, err := strconv.ParseUint(cleanStr, 16, 64)
	if err != nil {
		return 0, err
	}

	// 3. Generate Keystream Mask (64 bits)
	// Mask = SHA256(Key)[:8]
	keyHash := sha256.Sum256([]byte(secretKey))
	mask := binary.BigEndian.Uint64(keyHash[:8])

	// 4. Decrypt (XOR)
	decrypted := val ^ mask

	return decrypted, nil
}
