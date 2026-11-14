package server

import (
	"time"

	"github.com/auth-com-br/tend/internal/activity"
	"github.com/auth-com-br/tend/internal/session"
)

// ReportGuard writes down a command the guard stopped or put to the person
// (tend guard, the agents' PreToolUse hook; internal/policy's guard.go): a
// violation of the pane's run in the activity record, and a notice, as a
// policy an agent ran against is.
func (s *Server) ReportGuard(id session.PaneID, rule, outcome, message string) error {
	rt, err := s.runtime(id)
	if err != nil {
		return err
	}
	agentLabel := rt.status().Agent
	if s.activity != nil {
		r := s.activity
		r.mu.Lock()
		run := r.open[id]
		r.mu.Unlock()
		rec := activity.Record{
			Kind: activity.KindViolation, Time: time.Now(), Session: s.cfg.SessionName, Pane: uint64(id),
			Agent: agentLabel, Policy: rule, Message: message, Outcome: outcome,
		}
		if run != nil {
			// Its run's, so tend activity shows it beside the work it was
			// part of; a pane whose run the recorder has not seen yet
			// still gets the line.
			rec.Run, rec.Place = run.id, run.place
			if rec.Agent == "" {
				rec.Agent = run.agent
			}
		}
		_ = r.log.Append(rec)
	}
	title := "guard"
	if agentLabel != "" {
		title += ": " + agentLabel
	}
	s.publish(Event{Kind: EventNotify, Pane: id, Title: title, Body: message})
	return nil
}
