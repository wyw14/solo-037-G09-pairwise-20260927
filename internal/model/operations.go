package model

import "time"

type Operation struct {
    ID RunID `json:"id"`
    Name string `json:"name"`
    ModelName string `json:"model_name"`
    Section string `json:"section"`
    Carriage string `json:"carriage"`
    Conditions Conditions `json:"conditions"`
    Stage RunStage `json:"stage"`
    Revision RevisionID `json:"revision"`
    CreatedAt time.Time `json:"created_at"`
    UpdatedAt time.Time `json:"updated_at"`
}

func NewOperation(name string, modelName string, section string, carriage string, at time.Time) Operation {
    now := at.UTC()
    return Operation{
        ID: NewRunID(),
        Name: name,
        ModelName: modelName,
        Section: section,
        Carriage: carriage,
        Stage: StageConfigured,
        Revision: NewRevisionID(),
        CreatedAt: now,
        UpdatedAt: now,
    }
}

// WithConditions attaches the physical tow conditions. Conditions are fixed
// when the measured run is created; UpdatedAt is deliberately left untouched.
func (o Operation) WithConditions(conditions Conditions) Operation {
    o.Conditions = conditions.Normalized()
    return o
}

func (o Operation) Advance(to RunStage, at time.Time) (Operation, error) {
    stage, err := Advance(o.Stage, to)
    if err != nil {
        return o, err
    }
    o.Stage = stage
    o.UpdatedAt = at.UTC()
    return o, nil
}

func (o Operation) Abort(at time.Time) Operation {
    if !Terminal(o.Stage) {
        o.Stage = StageAborted
        o.UpdatedAt = at.UTC()
    }
    return o
}

func (o Operation) Active() bool {
    return !Terminal(o.Stage)
}

type Incident struct {
    ID EventID `json:"id"`
    Run RunID `json:"run"`
    Severity string `json:"severity"`
    Message string `json:"message"`
    CreatedAt time.Time `json:"created_at"`
}

func NewIncident(run RunID, severity string, message string, at time.Time) Incident {
    return Incident{ID: NewEventID(), Run: run, Severity: severity, Message: message, CreatedAt: at.UTC()}
}
