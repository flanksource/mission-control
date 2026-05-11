package main

import "time"

type ObjectRef struct {
	APIVersion string `json:"apiVersion,omitempty"`
	Kind       string `json:"kind"`
	Namespace  string `json:"namespace,omitempty"`
	Name       string `json:"name"`
}

type ExplainReport struct {
	Target      ObjectRef    `json:"target"`
	Summary     Summary      `json:"summary"`
	Runtime     Runtime      `json:"runtime"`
	Controllers []Controller `json:"controllers,omitempty"`
	Sources     []Source     `json:"sources,omitempty"`
	Renderers   []Renderer   `json:"renderers,omitempty"`
	Writers     []Writer     `json:"writers,omitempty"`
	Evidence    []Evidence   `json:"evidence,omitempty"`
}

type Summary struct {
	ManagedBy  string `json:"managedBy,omitempty"`
	Manager    string `json:"manager,omitempty"`
	Source     string `json:"source,omitempty"`
	Path       string `json:"path,omitempty"`
	Revision   string `json:"revision,omitempty"`
	Confidence int    `json:"confidence,omitempty"`
}

type Runtime struct {
	OwnerChain []ObjectRef `json:"ownerChain,omitempty"`
}

type Controller struct {
	Type       string `json:"type"`
	Kind       string `json:"kind"`
	Namespace  string `json:"namespace,omitempty"`
	Name       string `json:"name"`
	SyncStatus string `json:"syncStatus,omitempty"`
	Health     string `json:"health,omitempty"`
	Ready      string `json:"ready,omitempty"`
	Confidence int    `json:"confidence"`
}

type Source struct {
	Type     string `json:"type"`
	URL      string `json:"url,omitempty"`
	Path     string `json:"path,omitempty"`
	Revision string `json:"revision,omitempty"`
	Name     string `json:"name,omitempty"`
}

type Renderer struct {
	Type         string `json:"type"`
	Release      string `json:"release,omitempty"`
	Namespace    string `json:"namespace,omitempty"`
	Chart        string `json:"chart,omitempty"`
	ChartVersion string `json:"chartVersion,omitempty"`
}

type Writer struct {
	Manager    string     `json:"manager"`
	Operation  string     `json:"operation,omitempty"`
	APIVersion string     `json:"apiVersion,omitempty"`
	LastSeen   *time.Time `json:"lastSeen,omitempty"`
}

type Evidence struct {
	Detector   string `json:"detector"`
	Signal     string `json:"signal"`
	Key        string `json:"key,omitempty"`
	Value      string `json:"value,omitempty"`
	Message    string `json:"message,omitempty"`
	Confidence int    `json:"confidence,omitempty"`
}

func (r *ExplainReport) addEvidence(detector, signal, key, value, message string, confidence int) {
	r.Evidence = append(r.Evidence, Evidence{Detector: detector, Signal: signal, Key: key, Value: value, Message: message, Confidence: confidence})
}

func (r *ExplainReport) pickSummary() {
	var best *Controller
	for i := range r.Controllers {
		if best == nil || r.Controllers[i].Confidence > best.Confidence {
			best = &r.Controllers[i]
		}
	}
	if best == nil {
		return
	}
	r.Summary.ManagedBy = best.Type
	r.Summary.Manager = best.Kind + "/" + best.Name
	if best.Namespace != "" {
		r.Summary.Manager = best.Kind + "/" + best.Namespace + "/" + best.Name
	}
	r.Summary.Confidence = best.Confidence
	if len(r.Sources) > 0 {
		r.Summary.Source = r.Sources[0].URL
		if r.Summary.Source == "" {
			r.Summary.Source = r.Sources[0].Name
		}
		r.Summary.Path = r.Sources[0].Path
		r.Summary.Revision = r.Sources[0].Revision
	}
}
