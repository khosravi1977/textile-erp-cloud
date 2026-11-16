package main

import (
	"database/sql"
	"fmt"
	"net/http"
	"strings"
)

// basic handles the editable master-data lists. Historical operational rows
// intentionally keep their stored names when a list entry is renamed.
func (a *app) basic(w http.ResponseWriter, r *http.Request) {
	kind := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/basic/"), "/")
	table, idCol, nameCol, ok := basicMap(kind)
	if !ok {
		fail(w, http.StatusNotFound, "نوع اطلاعات اولیه معتبر نیست")
		return
	}
	if r.Method == http.MethodGet {
		writeJSON(w, a.lookup(table, idCol, nameCol))
		return
	}
	if r.Method != http.MethodPost && r.Method != http.MethodPut && r.Method != http.MethodDelete {
		w.Header().Set("Allow", "GET, POST, PUT, DELETE")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var payload struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	if !decode(w, r, &payload) {
		return
	}
	payload.Name = strings.TrimSpace(payload.Name)
	qTable, qID, qName := quoteIdent(table), quoteIdent(idCol), quoteIdent(nameCol)
	switch r.Method {
	case http.MethodPost:
		if payload.Name == "" {
			fail(w, http.StatusBadRequest, "نام را وارد کنید")
			return
		}
		var id int64
		var active int
		err := a.queryRow(fmt.Sprintf("SELECT %s, COALESCE(is_active,1) FROM %s WHERE %s=?", qID, qTable, qName), payload.Name).Scan(&id, &active)
		if err != nil && err != sql.ErrNoRows {
			fail(w, http.StatusInternalServerError, "خواندن اطلاعات اولیه انجام نشد")
			return
		}
		if err != nil {
			if _, err := a.exec(fmt.Sprintf("INSERT INTO %s (%s,is_active) VALUES (?,1)", qTable, qName), payload.Name); err != nil {
				fail(w, http.StatusInternalServerError, "ثبت اطلاعات اولیه انجام نشد")
				return
			}
		} else if active == 0 {
			if _, err := a.exec(fmt.Sprintf("UPDATE %s SET is_active=1 WHERE %s=?", qTable, qID), id); err != nil {
				fail(w, http.StatusInternalServerError, "بازگردانی اطلاعات اولیه انجام نشد")
				return
			}
		}
	case http.MethodPut:
		if payload.ID <= 0 || payload.Name == "" {
			fail(w, http.StatusBadRequest, "شناسه و نام معتبر را وارد کنید")
			return
		}
		var exists int
		if err := a.queryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s=? AND %s<>?", qTable, qName, qID), payload.Name, payload.ID).Scan(&exists); err != nil {
			fail(w, http.StatusInternalServerError, "بررسی نام اطلاعات اولیه انجام نشد")
			return
		}
		if exists > 0 {
			fail(w, http.StatusConflict, "این نام قبلاً ثبت شده است")
			return
		}
		tx, err := a.db.Begin()
		if err != nil {
			fail(w, http.StatusInternalServerError, "ویرایش اطلاعات اولیه انجام نشد")
			return
		}
		defer tx.Rollback()
		var oldName string
		if err := txQueryRow(a.dialect, tx, fmt.Sprintf("SELECT %s FROM %s WHERE %s=? AND COALESCE(is_active,1)=1", qName, qTable, qID), payload.ID).Scan(&oldName); err != nil {
			fail(w, http.StatusNotFound, "این گزینه دیگر وجود ندارد")
			return
		}
		result, err := txExec(a.dialect, tx, fmt.Sprintf("UPDATE %s SET %s=? WHERE %s=? AND COALESCE(is_active,1)=1", qTable, qName, qID), payload.Name, payload.ID)
		if err != nil {
			fail(w, http.StatusConflict, "ویرایش انجام نشد؛ نام تکراری است یا پایگاه داده آن را نپذیرفت")
			return
		}
		if affected, _ := result.RowsAffected(); affected == 0 {
			fail(w, http.StatusNotFound, "این گزینه دیگر وجود ندارد")
			return
		}
		if oldName != payload.Name {
			for _, ref := range basicTextReferences(kind) {
				if _, err := txExec(a.dialect, tx, fmt.Sprintf("UPDATE %s SET %s=? WHERE %s=?", quoteIdent(ref.table), quoteIdent(ref.column), quoteIdent(ref.column)), payload.Name, oldName); err != nil {
					fail(w, http.StatusInternalServerError, "به‌روزرسانی پیوندهای اطلاعات قبلی انجام نشد")
					return
				}
			}
		}
		if err := tx.Commit(); err != nil {
			fail(w, http.StatusInternalServerError, "ویرایش اطلاعات اولیه انجام نشد")
			return
		}
	case http.MethodDelete:
		if payload.ID <= 0 {
			fail(w, http.StatusBadRequest, "شناسه معتبر نیست")
			return
		}
		result, err := a.exec(fmt.Sprintf("UPDATE %s SET is_active=0 WHERE %s=? AND COALESCE(is_active,1)=1", qTable, qID), payload.ID)
		if err != nil {
			fail(w, http.StatusInternalServerError, "حذف از فهرست انجام نشد")
			return
		}
		if affected, _ := result.RowsAffected(); affected == 0 {
			fail(w, http.StatusNotFound, "این گزینه دیگر وجود ندارد یا قبلاً بایگانی شده است")
			return
		}
	}
	writeJSON(w, a.lookup(table, idCol, nameCol))
}

type basicTextReference struct {
	table  string
	column string
}

func basicTextReferences(kind string) []basicTextReference {
	refs := map[string][]basicTextReference{
		"customers": {
			{"nakh_vor", "moshname_nakh_vor"}, {"nakh_khor", "moshname_nakh_khor"},
			{"nakh_khor", "owner_mosh_nakh_khor"}, {"nakh_salon", "mosh_name_nakh_salon"},
			{"chelle", "mosh_chelle"}, {"f_khor", "mosh_f_khor"},
		},
		"yarns": {
			{"nakh_vor", "nakh_name_nakh_vor"}, {"nakh_khor", "nakh_name_nakh_khor"},
			{"nakh_salon", "nakh_name_nakh_salon"}, {"chelle", "nakh_chelle"},
		},
		"fabrics": {
			{"salon", "kala_salon"}, {"chelle_formul", "kala_name"}, {"f_khor", "kala_name_f_khor"},
		},
		"warpers": {{"chelle", "pich_chelle"}, {"empty_beam_out", "chellepich_name"}},
		"beams":   {{"chelle", "codnavard_chelle"}, {"empty_beam_out", "kod_navard"}},
		"costs":   {{"h_rozmare", "onvan_hazine"}},
		"operators": {
			{"machinery_service", "operator_name"}, {"production_waste", "operator_name"}, {"h_rozmare", "operator_name"},
		},
		"tiers":   {{"gere", "name_gere"}},
		"weavers": {{"h_rozmare", "weaver_name"}},
	}
	return refs[kind]
}
