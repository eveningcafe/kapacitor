package alertmanager

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"path"
	"sync/atomic"
	text "text/template"
	"time"

	"github.com/influxdata/kapacitor/alert"
	khttp "github.com/influxdata/kapacitor/http"
	"github.com/influxdata/kapacitor/keyvalue"
	"github.com/influxdata/kapacitor/models"
	"github.com/pkg/errors"
)

const (
	defaultResource = "{{ .Name }}"
	defaultEvent    = "{{ .ID }}"
	defaultGroup    = "{{ .Group }}"
	defaultTimeout  = time.Duration(24 * time.Hour)
)

type Diagnostic interface {
	WithContext(ctx ...keyvalue.T) Diagnostic
	TemplateError(err error, kv keyvalue.T)
	Error(msg string, err error)
}

type Service struct {
	configValue atomic.Value
	clientValue atomic.Value
	diag        Diagnostic
}

func NewService(c Config, d Diagnostic) *Service {
	s := &Service{
		diag: d,
	}
	s.configValue.Store(c)
	s.clientValue.Store(&http.Client{
		Transport: khttp.NewDefaultTransportWithTLS(&tls.Config{InsecureSkipVerify: c.InsecureSkipVerify}),
	})
	return s
}

type AlertmanagerRequest struct {
	Status      string                  `json:"status"`
	Labels      AlertmanagerLabels      `json:"labels"`
	Annotations AlertmanagerAnnotations `json:"annotations"`
}
type AlertmanagerLabels struct {
	Instance    string   `json:"instance"`
	Event       string   `json:"event"`
	Environment string   `json:"environment"`
	Origin      string   `json:"origin"`
	Service     []string `json:"service"`
	Group       string   `json:"group"`
	Customer    string   `json:"customer"`
}
type AlertmanagerAnnotations struct {
	Summary  string `json:"summary"`
	Value    string `json:"value"`
	Severity string `json:"severity"`
}

type testOptions struct {
	Resource    string   `json:"resource"`
	Event       string   `json:"event"`
	Environment string   `json:"environment"`
	Severity    string   `json:"severity"`
	Group       string   `json:"group"`
	Customer    string   `json:"customer"`
	Value       string   `json:"value"`
	Message     string   `json:"message"`
	Origin      string   `json:"origin"`
	Service     []string `json:"service"`
	Timeout     string   `json:"timeout"`
}

func (s *Service) TestOptions() interface{} {
	c := s.config()
	return &testOptions{
		Resource:    "testResource",
		Event:       "testEvent",
		Environment: c.Environment,
		Severity:    "critical",
		Group:       "testGroup",
		Value:       "testValue",
		Message:     "test alertmanager message",
		Origin:      c.Origin,
		Service:     []string{"testServiceA", "testServiceB"},
		Timeout:     "24h0m0s",
	}
}

func (s *Service) Test(options interface{}) error {
	o, ok := options.(*testOptions)
	if !ok {
		return fmt.Errorf("unexpected options type %T", options)
	}
	timeout, _ := time.ParseDuration(o.Timeout)
	return s.Alert("firing",
		o.Resource,
		o.Event,
		o.Environment,
		o.Severity,
		o.Group,
		o.Customer,
		o.Value,
		o.Message,
		o.Origin,
		o.Service,
		timeout,
		map[string]string{},
		models.Result{},
	)
}

func (s *Service) Open() error {
	return nil
}

func (s *Service) Close() error {
	return nil
}

func (s *Service) config() Config {
	return s.configValue.Load().(Config)
}

func (s *Service) Update(newConfig []interface{}) error {
	if l := len(newConfig); l != 1 {
		return fmt.Errorf("expected only one new config object, got %d", l)
	}
	if c, ok := newConfig[0].(Config); !ok {
		return fmt.Errorf("expected config object to be of type %T, got %T", c, newConfig[0])
	} else {
		s.configValue.Store(c)
		s.clientValue.Store(&http.Client{
			Transport: khttp.NewDefaultTransportWithTLS(&tls.Config{InsecureSkipVerify: c.InsecureSkipVerify}),
		})
	}

	return nil
}

func (s *Service) Alert(status, resource, event, environment, severity, group, customer, value, message, origin string, service []string, timeout time.Duration, tags map[string]string, data models.Result) error {
	if resource == "" || event == "" {
		return errors.New("Resource and Event are required to send an alert")
	}

	req, err := s.preparePost(status, resource, event, environment, severity, group, customer, value, message, origin, service, timeout, tags, data)
	if err != nil {
		return err
	}

	client := s.clientValue.Load().(*http.Client)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		type response struct {
			Message string `json:"message"`
		}
		r := &response{Message: fmt.Sprintf("failed to understand Alertmanager response. code: %d content: %s", resp.StatusCode, string(body))}
		b := bytes.NewReader(body)
		dec := json.NewDecoder(b)
		dec.Decode(r)
		return errors.New(r.Message)
	}
	return nil
}

func (s *Service) preparePost(status, resource, event, environment, severity, group, customer, value, message, origin string, service []string, timeout time.Duration, tags map[string]string, data models.Result) (*http.Request, error) {
	c := s.config()

	if !c.Enabled {
		return nil, errors.New("service is not enabled")
	}

	if environment == "" {
		environment = c.Environment
	}

	if origin == "" {
		origin = c.Origin
	}

	u, err := url.Parse(c.URL)
	if err != nil {
		return nil, err
	}
	u.Path = path.Join(u.Path, "alert")

	//postData := make(map[string]interface{})
	//postData["resource"] = resource
	//postData["event"] = event
	//postData["environment"] = environment
	//postData["severity"] = severity
	//postData["group"] = group
	//postData["value"] = value
	//postData["text"] = message
	//postData["origin"] = origin
	//postData["rawData"] = data
	//if len(service) > 0 {
	//	postData["service"] = service
	//}
	//postData["timeout"] = int64(timeout / time.Second)
	//
	//tagList := make([]string, 0)
	//for k, v := range tags {
	//	tagList = append(tagList, fmt.Sprintf("%s=%s", k, v))
	//}
	//postData["tags"] = tagList
	//
	//var post bytes.Buffer
	//enc := json.NewEncoder(&post)
	//err = enc.Encode(postData)
	//if err != nil {
	//	return nil, err
	//}

	//req, err := http.NewRequest("POST", u.String(), &post)
	bodyData := AlertmanagerRequest{}

	bodyData.Status = status
	bodyData.Annotations.Severity = severity
	bodyData.Annotations.Summary = message
	bodyData.Annotations.Value = value

	bodyData.Labels.Group = group
	bodyData.Labels.Service = service
	bodyData.Labels.Customer = customer
	bodyData.Labels.Environment = environment
	bodyData.Labels.Event = event
	bodyData.Labels.Instance = resource
	bodyData.Labels.Origin = origin

	jsonBody, err := json.Marshal([]AlertmanagerRequest{bodyData})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", u.String(), bytes.NewBuffer(jsonBody))

	req.Header.Add("Content-Type", "application/json")
	if err != nil {
		return nil, err
	}

	return req, nil
}

type HandlerConfig struct {
	// Alertmanager resource.
	// Can be a template and has access to the same data as the AlertNode.Details property.
	// Default: {{ .Name }}
	Resource string `mapstructure:"resource"`

	// Alertmanager event.
	// Can be a template and has access to the same data as the idInfo property.
	// Default: {{ .ID }}
	Event string `mapstructure:"event"`

	// Alertmanager environment.
	// Can be a template and has access to the same data as the AlertNode.Details property.
	// Defaut is set from the configuration.
	Environment string `mapstructure:"environment"`

	// Alertmanager group.
	// Can be a template and has access to the same data as the AlertNode.Details property.
	// Default: {{ .Group }}
	Group string `mapstructure:"group"`

	// Alertmanager group.
	// Can be a template and has access to the same data as the AlertNode.Details property.
	// Default: {{ .Group }}
	Customer string `mapstructure:"customer"`

	// Alertmanager value.
	// Can be a template and has access to the same data as the AlertNode.Details property.
	// Default is an empty string.
	Value string `mapstructure:"value"`

	// Alertmanager origin.
	// If empty uses the origin from the configuration.
	Origin string `mapstructure:"origin"`

	// List of effected Services
	Service []string `mapstructure:"service"`

	// Alertmanager timeout.
	// Default: 24h
	Timeout time.Duration `mapstructure:"timeout"`
}

type handler struct {
	s    *Service
	c    HandlerConfig
	diag Diagnostic

	resourceTmpl    *text.Template
	eventTmpl       *text.Template
	environmentTmpl *text.Template
	valueTmpl       *text.Template
	groupTmpl       *text.Template
	customerTmpl    *text.Template
	serviceTmpl     []*text.Template
}

func (s *Service) DefaultHandlerConfig() HandlerConfig {
	return HandlerConfig{
		Resource: defaultResource,
		Event:    defaultEvent,
		Group:    defaultGroup,
		Timeout:  defaultTimeout,
	}
}

func (s *Service) Handler(c HandlerConfig, ctx ...keyvalue.T) (alert.Handler, error) {
	// Parse and validate alertmanager templates
	rtmpl, err := text.New("resource").Parse(c.Resource)
	if err != nil {
		return nil, err
	}
	evtmpl, err := text.New("event").Parse(c.Event)
	if err != nil {
		return nil, err
	}
	etmpl, err := text.New("environment").Parse(c.Environment)
	if err != nil {
		return nil, err
	}
	gtmpl, err := text.New("group").Parse(c.Group)
	if err != nil {
		return nil, err
	}

	ctmpl, err := text.New("customer").Parse(c.Customer)
	if err != nil {
		return nil, err
	}

	vtmpl, err := text.New("value").Parse(c.Value)
	if err != nil {
		return nil, err
	}

	var stmpl []*text.Template
	for _, service := range c.Service {
		tmpl, err := text.New("service").Parse(service)
		if err != nil {
			return nil, err
		}
		stmpl = append(stmpl, tmpl)
	}

	return &handler{
		s:               s,
		c:               c,
		diag:            s.diag.WithContext(ctx...),
		resourceTmpl:    rtmpl,
		eventTmpl:       evtmpl,
		environmentTmpl: etmpl,
		groupTmpl:       gtmpl,
		customerTmpl:    ctmpl,
		valueTmpl:       vtmpl,
		serviceTmpl:     stmpl,
	}, nil
}

type eventData struct {
	ID string
	// Measurement name
	Name string

	// Task name
	TaskName string

	// Concatenation of all group-by tags of the form [key=value,]+.
	// If not groupBy is performed equal to literal 'nil'
	Group string

	// Map of tags
	Tags map[string]string
}

func (h *handler) Handle(event alert.Event) {
	td := event.TemplateData()
	var buf bytes.Buffer
	err := h.resourceTmpl.Execute(&buf, td)
	if err != nil {
		h.diag.TemplateError(err, keyvalue.KV("resource", h.c.Resource))
		return
	}
	resource := buf.String()
	buf.Reset()

	data := eventData{
		ID:       td.ID,
		Name:     td.Name,
		TaskName: td.TaskName,
		Group:    td.Group,
		Tags:     td.Tags,
	}
	err = h.eventTmpl.Execute(&buf, data)
	if err != nil {
		h.diag.TemplateError(err, keyvalue.KV("event", h.c.Event))
		return
	}
	eventStr := buf.String()
	buf.Reset()

	err = h.environmentTmpl.Execute(&buf, td)
	if err != nil {
		h.diag.TemplateError(err, keyvalue.KV("environment", h.c.Environment))
		return
	}
	environment := buf.String()
	buf.Reset()

	err = h.groupTmpl.Execute(&buf, td)
	if err != nil {
		h.diag.TemplateError(err, keyvalue.KV("group", h.c.Group))
		return
	}
	group := buf.String()
	buf.Reset()

	err = h.customerTmpl.Execute(&buf, td)
	if err != nil {
		h.diag.TemplateError(err, keyvalue.KV("customer", h.c.Customer))
		return
	}
	customer := buf.String()
	buf.Reset()

	err = h.valueTmpl.Execute(&buf, td)
	if err != nil {
		h.diag.TemplateError(err, keyvalue.KV("value", h.c.Value))
		return
	}
	value := buf.String()
	buf.Reset()

	var service []string
	if len(h.serviceTmpl) == 0 {
		service = []string{td.Name}
	} else {
		for _, tmpl := range h.serviceTmpl {
			err = tmpl.Execute(&buf, td)
			if err != nil {
				h.diag.TemplateError(err, keyvalue.KV("service", tmpl.Name()))
				return
			}
			service = append(service, buf.String())
			buf.Reset()
		}
	}

	var severity string
	var status string
	switch event.State.Level {
	case alert.OK:
		severity = "ok"
		status = "resolved"
	case alert.Info:
		severity = "informational"
		status = "firing"
	case alert.Warning:
		severity = "warning"
		status = "firing"
	case alert.Critical:
		severity = "critical"
		status = "firing"
	default:
		severity = "indeterminate"
		status = "firing"
	}

	if err := h.s.Alert(
		status,
		resource,
		eventStr,
		environment,
		severity,
		group,
		customer,
		value,
		event.State.Message,
		h.c.Origin,
		service,
		h.c.Timeout,
		event.Data.Tags,
		event.Data.Result,
	); err != nil {
		h.diag.Error("failed to send event to Alertmanager", err)
	}
}
