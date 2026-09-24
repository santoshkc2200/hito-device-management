package apiserver

import (
	"net/http"

	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
	"github.com/hito-hospital/hdms/internal/platform/settings"
)

// GetSettings returns policy, label template, and paper slip template.
func (s *Server) GetSettings(w http.ResponseWriter, r *http.Request) {
	st, err := s.settings.GetSettings(r.Context())
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, mapSettingsToGen(st))
}

// UpdateSettings replaces one or more settings sections wholesale.
func (s *Server) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	body, ok := decodeJSON[gen.UpdateSettingsRequest](w, r)
	if !ok {
		return
	}

	actor := actorFrom(r)
	var params settings.UpdateSettingsParams

	if body.Policy != nil {
		p := body.Policy
		params.Policy = &settings.PolicySettings{
			BlockOnOverdue:            p.BlockOnOverdue,
			SessionIdleTimeoutSeconds: p.SessionIdleTimeoutSeconds,
			KioskSoundEnabled:         p.KioskSoundEnabled,
			LowStockThreshold:         p.LowStockThreshold,
			PaperBacklogHours:         p.PaperBacklogHours,
		}
	}
	if body.BookingPolicy != nil {
		params.BookingPolicy = &settings.BookingPolicySettings{
			AdvanceDays:         body.BookingPolicy.AdvanceDays,
			MaxDurationDays:     body.BookingPolicy.MaxDurationDays,
			ReturnBufferMinutes: body.BookingPolicy.ReturnBufferMinutes,
		}
	}

	if body.LabelTemplate != nil {
		lt := body.LabelTemplate
		params.LabelTemplate = &settings.LabelTemplateSettings{
			SheetWidthMm:  float64(lt.SheetWidthMm),
			SheetHeightMm: float64(lt.SheetHeightMm),
			Columns:       lt.Columns,
			Rows:          lt.Rows,
			MarginTopMm:   float64(lt.MarginTopMm),
			MarginLeftMm:  float64(lt.MarginLeftMm),
			GutterXMm:     float64(lt.GutterXMm),
			GutterYMm:     float64(lt.GutterYMm),
			LabelWidthMm:  float64(lt.LabelWidthMm),
			LabelHeightMm: float64(lt.LabelHeightMm),
		}
	}

	if body.SlipTemplate != nil {
		st := body.SlipTemplate
		params.SlipTemplate = &settings.SlipTemplateSettings{
			HospitalName:  st.HospitalName,
			PageRefFormat: st.PageRefFormat,
			RowsPerPage:   st.RowsPerPage,
			Columns:       st.Columns,
		}
	}

	updated, err := s.settings.UpdateSettings(r.Context(), params, actor)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, mapSettingsToGen(updated))
}

func mapSettingsToGen(st settings.Settings) gen.Settings {
	return gen.Settings{
		Policy: gen.PolicySettings{
			BlockOnOverdue:            st.Policy.BlockOnOverdue,
			SessionIdleTimeoutSeconds: st.Policy.SessionIdleTimeoutSeconds,
			KioskSoundEnabled:         st.Policy.KioskSoundEnabled,
			LowStockThreshold:         st.Policy.LowStockThreshold,
			PaperBacklogHours:         st.Policy.PaperBacklogHours,
		},
		BookingPolicy: gen.BookingPolicySettings{
			AdvanceDays:         st.BookingPolicy.AdvanceDays,
			MaxDurationDays:     st.BookingPolicy.MaxDurationDays,
			ReturnBufferMinutes: st.BookingPolicy.ReturnBufferMinutes,
		},
		LabelTemplate: gen.LabelTemplateSettings{
			SheetWidthMm:  float32(st.LabelTemplate.SheetWidthMm),
			SheetHeightMm: float32(st.LabelTemplate.SheetHeightMm),
			Columns:       st.LabelTemplate.Columns,
			Rows:          st.LabelTemplate.Rows,
			MarginTopMm:   float32(st.LabelTemplate.MarginTopMm),
			MarginLeftMm:  float32(st.LabelTemplate.MarginLeftMm),
			GutterXMm:     float32(st.LabelTemplate.GutterXMm),
			GutterYMm:     float32(st.LabelTemplate.GutterYMm),
			LabelWidthMm:  float32(st.LabelTemplate.LabelWidthMm),
			LabelHeightMm: float32(st.LabelTemplate.LabelHeightMm),
		},
		SlipTemplate: gen.SlipTemplateSettings{
			HospitalName:  st.SlipTemplate.HospitalName,
			PageRefFormat: st.SlipTemplate.PageRefFormat,
			RowsPerPage:   st.SlipTemplate.RowsPerPage,
			Columns:       st.SlipTemplate.Columns,
		},
		UpdatedAt: st.UpdatedAt,
		UpdatedBy: strPtr(st.UpdatedBy),
	}
}
