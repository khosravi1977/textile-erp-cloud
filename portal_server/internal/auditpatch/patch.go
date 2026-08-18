package auditpatch

import (
	"bytes"
	"fmt"
)

type replacement struct {
	label string
	old   []byte
	new   []byte
}

var replacements = []replacement{
	{
		label: "accessResponse must never recover or expose a password",
		old: []byte("\tif accessRequiresSetup(record) {\n\t\trawPassword = \"\"\n\t} else if rawPassword == \"\" {\n\t\trawPassword = a.portalAccessPassword(record)\n\t}\n"),
		new: []byte("\trawPassword = \"\"\n"),
	},
	{
		label: "admin edit form must never prefill a password",
		old:   []byte("      form.password.value = row.password || '';"),
		new:   []byte("      form.password.value = '';"),
	},
	{
		label: "team page must explain reset-only password handling",
		old:   []byte("نام، نقش و بخش‌های مجاز را انتخاب کنید؛ سپس نام کاربری و رمز را با دکمه کپی برای کارمند بفرستید."),
		new:   []byte("نام، نقش و بخش‌های مجاز را انتخاب کنید. رمز پس از ذخیره قابل مشاهده نیست؛ برای تغییر رمز، کاربر را ویرایش و رمز جدید تعیین کنید."),
	},
}

func Transform(source []byte) ([]byte, error) {
	out := append([]byte(nil), source...)
	for _, item := range replacements {
		count := bytes.Count(out, item.old)
		if count == 0 && bytes.Count(out, item.new) == 1 {
			continue
		}
		if count != 1 {
			return nil, fmt.Errorf("%s: expected exactly one source pattern, found %d", item.label, count)
		}
		out = bytes.Replace(out, item.old, item.new, 1)
	}
	return out, nil
}

func IsHardened(source []byte) bool {
	if bytes.Contains(source, []byte("rawPassword = a.portalAccessPassword(record)")) {
		return false
	}
	if bytes.Contains(source, []byte("form.password.value = row.password || ''")) {
		return false
	}
	return bytes.Contains(source, []byte("rawPassword = \"\""))
}
