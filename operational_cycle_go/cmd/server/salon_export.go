package main

func salonListQuery(all bool) string {
	query := `SELECT s.id_salon, s.tarikh_salon, s.metr_salon, s.w_salon, s.machin_salon, s.user_salon, s.kala_salon, s.ham_pod_salon, s.ham_chelle_salon, s.shom_chelle_salon, COALESCE(k.id_kala_name,0), COALESCE(s.chelle_id_salon,0), COALESCE(s.tar_percent_salon,50), COALESCE(s.pod_percent_salon,50) FROM salon s LEFT JOIN kala_name k ON k.name_kala_name=s.kala_salon ORDER BY s.id_salon DESC`
	if !all {
		query += ` LIMIT 200`
	}
	return query
}
