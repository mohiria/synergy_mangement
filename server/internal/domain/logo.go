package domain

import "errors"

// 系统 logo 规则（模块 PRD §7.1；#211）：仅 PNG／JPG／WebP，≤512KB，不收 SVG（可内嵌脚本）。
const MaxLogoBytes = 512 * 1024

var (
	ErrLogoTypeUnsupported = errors.New("logo 仅支持 PNG、JPG、WebP")
	ErrLogoTooLarge        = errors.New("logo 不能超过 512KB")
	ErrLogoEmpty           = errors.New("logo 文件为空")
)

// allowedLogoTypes 按内容探测（http.DetectContentType）得到的类型；不信文件名与客户端声明。
var allowedLogoTypes = map[string]struct{}{"image/png": {}, "image/jpeg": {}, "image/webp": {}}

// ValidateLogo 校验 logo（#211）：非空、类型在白名单、不超 512KB。
func ValidateLogo(detectedType string, size int64) error {
	if size <= 0 {
		return ErrLogoEmpty
	}
	if _, ok := allowedLogoTypes[detectedType]; !ok {
		return ErrLogoTypeUnsupported
	}
	if size > MaxLogoBytes {
		return ErrLogoTooLarge
	}
	return nil
}
