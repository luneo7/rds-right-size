package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/config"
	tea "github.com/charmbracelet/bubbletea"
	cwTypes "github.com/luneo7/rds-right-size/internal/cw/types"
	"github.com/luneo7/rds-right-size/internal/export"
	"github.com/luneo7/rds-right-size/internal/generator"
	rds "github.com/luneo7/rds-right-size/internal/rds-right-size"
	"github.com/luneo7/rds-right-size/internal/rds-right-size/types"
)

type screen int

const (
	screenConfig screen = iota
	screenGenerate
	screenLoading
	screenGenerating
	screenResults
	screenDetail
)

// GenerateDoneMsg is sent when the instance types generation completes.
type GenerateDoneMsg struct {
	Err        error
	OutputPath string
}

// GenerateStatusMsg is sent during generation to update progress text.
type GenerateStatusMsg struct {
	Status string
}

type Model struct {
	currentScreen screen
	config        ConfigModel
	generate      GenerateModel
	loading       LoadingModel
	results       ResultsModel
	detail        DetailModel
	width         int
	height        int

	// Analysis state
	recommendations []types.Recommendation
	progressChan    chan ProgressMsg
	region          string // AWS region from config, used for pricing lookups and exports
}

func NewModel(defaults ConfigValues) Model {
	return Model{
		currentScreen: screenConfig,
		config:        NewConfigModel(defaults),
		loading:       NewLoadingModel(),
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.config.Init(),
		tea.WindowSize(),
	)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		// Propagate to ALL child models regardless of active screen
		var cmds []tea.Cmd
		var cmd tea.Cmd
		m.config, cmd = m.config.Update(msg)
		cmds = append(cmds, cmd)
		m.generate, cmd = m.generate.Update(msg)
		cmds = append(cmds, cmd)
		m.loading, cmd = m.loading.Update(msg)
		cmds = append(cmds, cmd)
		m.results, cmd = m.results.Update(msg)
		cmds = append(cmds, cmd)
		m.detail, cmd = m.detail.Update(msg)
		cmds = append(cmds, cmd)
		return m, tea.Batch(cmds...)

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "q":
			if m.currentScreen != screenConfig && m.currentScreen != screenGenerate {
				return m, tea.Quit
			}
		}
	}

	switch m.currentScreen {
	case screenConfig:
		return m.updateConfig(msg)
	case screenGenerate:
		return m.updateGenerate(msg)
	case screenLoading:
		return m.updateLoading(msg)
	case screenGenerating:
		return m.updateGenerating(msg)
	case screenResults:
		return m.updateResults(msg)
	case screenDetail:
		return m.updateDetail(msg)
	}

	return m, nil
}

func (m Model) View() string {
	switch m.currentScreen {
	case screenConfig:
		return m.config.View()
	case screenGenerate:
		return m.generate.View()
	case screenLoading:
		return m.loading.View()
	case screenGenerating:
		return m.loading.View()
	case screenResults:
		return m.results.View()
	case screenDetail:
		return m.detail.View()
	}
	return ""
}

func (m Model) updateConfig(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q":
			// Only quit if not focused on an input field
			if m.config.focusIndex == fieldSubmit {
				return m, tea.Quit
			}
		case "enter":
			if m.config.focusIndex == fieldSubmit {
				return m.startAnalysis()
			}
		case "ctrl+r":
			return m.startAnalysis()
		case "ctrl+u":
			return m.openGenerate()
		}
	}

	var cmd tea.Cmd
	m.config, cmd = m.config.Update(msg)
	return m, cmd
}

func (m Model) openGenerate() (tea.Model, tea.Cmd) {
	// Read the current region from config to pass to the generation dialog
	region := m.config.inputs[fieldRegion].Value()

	m.generate = NewGenerateModel(region)
	m.generate.width = m.width
	m.generate.height = m.height
	m.currentScreen = screenGenerate

	return m, m.generate.Init()
}

func (m Model) updateGenerate(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg.(type) {
	case GenerateCancelMsg:
		m.currentScreen = screenConfig
		return m, m.config.Init()

	case GenerateSubmitMsg:
		return m.startGeneration(msg.(GenerateSubmitMsg))
	}

	var cmd tea.Cmd
	m.generate, cmd = m.generate.Update(msg)
	return m, cmd
}

func (m Model) updateLoading(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "esc" {
			m.currentScreen = screenConfig
			return m, m.config.Init()
		}

	case ProgressMsg:
		m.loading, _ = m.loading.Update(msg)
		return m, waitForProgress(m.progressChan)

	case AnalysisDoneMsg:
		if msg.Err != nil {
			m.config.err = msg.Err
			m.currentScreen = screenConfig
			return m, m.config.Init()
		}
		m.recommendations = msg.Recommendations
		m.currentScreen = screenResults
		m.results = NewResultsModel(m.recommendations)
		m.results.warnings = msg.Warnings
		m.results.width = m.width
		m.results.height = m.height
		return m, nil
	}

	var cmd tea.Cmd
	m.loading, cmd = m.loading.Update(msg)
	return m, cmd
}

func (m Model) updateGenerating(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "esc" {
			m.currentScreen = screenConfig
			return m, m.config.Init()
		}

	case GenerateStatusMsg:
		m.loading = m.loading.SetStatus(msg.Status)
		return m, nil

	case GenerateDoneMsg:
		if msg.Err != nil {
			m.config.err = msg.Err
		} else {
			// Update the instance types field with the generated file path
			m.config.inputs[fieldInstanceTypes].SetValue(msg.OutputPath)
			m.config.err = nil
		}
		m.currentScreen = screenConfig
		return m, m.config.Init()
	}

	var cmd tea.Cmd
	m.loading, cmd = m.loading.Update(msg)
	return m, cmd
}

func (m Model) updateResults(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			rec := m.results.SelectedRecommendation()
			if rec != nil {
				m.currentScreen = screenDetail
				m.detail = NewDetailModel(rec, m.width, m.height)
				return m, nil
			}
		case "b":
			m.currentScreen = screenConfig
			return m, m.config.Init()
		case "e":
			path, err := rds.WriteRecommendationsJSON(m.recommendations)
			if err != nil {
				m.results = m.results.SetExportErr(err.Error())
			} else {
				m.results = m.results.SetExportPath(path)
			}
			return m, nil
		case "p":
			// Export selected instance as PNG
			rec := m.results.SelectedRecommendation()
			if rec != nil {
				region := rec.Region
				if region == "" {
					region = m.region
				}
				if region == "" {
					region = export.RegionFromAZ(rec.AvailabilityZone)
				}
				path, err := export.ExportInstancePNG(rec, region, ".")
				if err != nil {
					m.results = m.results.SetExportErr(err.Error())
				} else {
					m.results = m.results.SetExportPath(path)
				}
			}
			return m, nil
		case "P":
			// Export all instances in the selected instance's cluster as PNG
			rec := m.results.SelectedRecommendation()
			if rec != nil && rec.DBClusterIdentifier != nil && *rec.DBClusterIdentifier != "" {
				clusterID := *rec.DBClusterIdentifier
				var clusterRecs []types.Recommendation
				for _, r := range m.recommendations {
					if r.DBClusterIdentifier != nil && *r.DBClusterIdentifier == clusterID {
						clusterRecs = append(clusterRecs, r)
					}
				}
				region := rec.Region
				if region == "" {
					region = m.region
				}
				if region == "" {
					region = export.RegionFromAZ(rec.AvailabilityZone)
				}
				path, err := export.ExportClusterPNG(clusterRecs, clusterID, region, ".")
				if err != nil {
					m.results = m.results.SetExportErr(err.Error())
				} else {
					m.results = m.results.SetExportPath(path)
				}
			} else {
				m.results = m.results.SetExportErr("Selected instance is not part of a cluster")
			}
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.results, cmd = m.results.Update(msg)
	return m, cmd
}

func (m Model) updateDetail(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc", "b":
			m.currentScreen = screenResults
			return m, nil
		case "p":
			// Export current instance detail as PNG
			rec := m.detail.recommendation
			if rec != nil {
				region := rec.Region
				if region == "" {
					region = m.region
				}
				if region == "" {
					region = export.RegionFromAZ(rec.AvailabilityZone)
				}
				path, err := export.ExportInstancePNG(rec, region, ".")
				if err != nil {
					m.detail = m.detail.SetExportStatus("Export error: " + err.Error())
				} else {
					m.detail = m.detail.SetExportStatus("Exported: " + path)
				}
			}
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.detail, cmd = m.detail.Update(msg)
	return m, cmd
}

func (m Model) startAnalysis() (tea.Model, tea.Cmd) {
	values, err := m.config.GetValues()
	if err != nil {
		m.config.err = err
		return m, nil
	}

	m.currentScreen = screenLoading
	m.loading = NewLoadingModel()
	m.loading.width = m.width
	m.loading.height = m.height
	m.progressChan = make(chan ProgressMsg, 100)

	// Store first region as fallback for exports; each recommendation carries its own Region
	regions := splitRegions(values.Region)
	if len(regions) > 0 {
		m.region = regions[0]
	} else {
		m.region = values.Region
	}

	return m, tea.Batch(
		m.loading.Init(),
		m.runAnalysis(values),
		waitForProgress(m.progressChan),
	)
}

func (m Model) startGeneration(submit GenerateSubmitMsg) (tea.Model, tea.Cmd) {
	region := m.config.inputs[fieldRegion].Value()
	profile := m.config.inputs[fieldProfile].Value()

	// Use first region for generation (generation always uses a single region)
	if parts := splitRegions(region); len(parts) > 0 {
		region = parts[0]
	}

	if region == "" {
		m.generate.err = fmt.Errorf("AWS Region is required — set it in the configuration screen")
		return m, nil
	}

	m.currentScreen = screenGenerating
	m.loading = NewLoadingModel()
	m.loading.width = m.width
	m.loading.height = m.height
	m.loading = m.loading.SetStatus("Generating instance types...")

	return m, tea.Batch(
		m.loading.Init(),
		m.runGeneration(profile, region, submit),
	)
}

func (m Model) runGeneration(profile, region string, submit GenerateSubmitMsg) tea.Cmd {
	return func() tea.Msg {
		var optFns []func(*config.LoadOptions) error
		if profile != "" {
			optFns = append(optFns, config.WithSharedConfigProfile(profile))
		}
		if region != "" {
			optFns = append(optFns, config.WithRegion(region))
		}

		cfg, err := config.LoadDefaultConfig(context.Background(), optFns...)
		if err != nil {
			return GenerateDoneMsg{Err: err}
		}

		opts := generator.GenerateOptions{
			Engine:        submit.Engine,
			Region:        region,
			TargetRegions: submit.TargetRegions,
			Output:        submit.OutputFile,
			OnStatus: func(status string) {
				// Note: OnStatus runs synchronously inside GenerateInstanceTypes,
				// but we can't send tea.Msgs from here since we're already in a Cmd.
			},
		}

		if err := generator.GenerateInstanceTypes(context.Background(), cfg, opts); err != nil {
			return GenerateDoneMsg{Err: err}
		}

		return GenerateDoneMsg{OutputPath: submit.OutputFile}
	}
}

func (m Model) runAnalysis(values ConfigValues) tea.Cmd {
	progressChan := m.progressChan

	return func() tea.Msg {
		regions := splitRegions(values.Region)
		tags := parseTags(values.Tags)

		// Single region (or no region specified) — existing behavior
		if len(regions) <= 1 {
			region := values.Region

			var optFns []func(*config.LoadOptions) error
			if values.Profile != "" {
				optFns = append(optFns, config.WithSharedConfigProfile(values.Profile))
			}
			if region != "" {
				optFns = append(optFns, config.WithRegion(region))
			}

			cfg, err := config.LoadDefaultConfig(context.Background(), optFns...)
			if err != nil {
				close(progressChan)
				return AnalysisDoneMsg{Err: err}
			}

			analyzer := rds.NewRDSRightSize(
				&values.InstanceTypesURL,
				&cfg,
				values.Period,
				tags,
				values.CPUDownsize,
				values.CPUUpsize,
				values.MemUpsize,
				cwTypes.StatName(values.Stat),
				values.PreferNewGen,
				region,
			)

			var warnings []string
			opts := &rds.AnalysisOptions{
				FetchTimeSeries: true,
				OnProgress: func(current int, total int, instanceId string) {
					progressChan <- ProgressMsg{
						Current:    current,
						Total:      total,
						InstanceID: instanceId,
					}
				},
				OnWarning: func(instanceId, msg string) {
					warnings = append(warnings, fmt.Sprintf("%s: %s", instanceId, msg))
				},
			}

			recommendations, err := analyzer.AnalyzeRDS(opts)
			close(progressChan)
			if err != nil {
				return AnalysisDoneMsg{Err: err}
			}

			// Stamp region on each recommendation
			for i := range recommendations {
				recommendations[i].Region = region
			}

			return AnalysisDoneMsg{Recommendations: recommendations, Warnings: warnings}
		}

		// Multi-region parallel analysis
		type regionResult struct {
			region          string
			recommendations []types.Recommendation
			err             error
		}

		var mu sync.Mutex
		type progress struct{ current, total int }
		regionProg := make(map[string]*progress)
		var allWarnings []string

		results := make([]regionResult, len(regions))
		var wg sync.WaitGroup

		for i, rgn := range regions {
			regionProg[rgn] = &progress{}
			wg.Add(1)
			go func(idx int, rgn string) {
				defer wg.Done()

				var optFns []func(*config.LoadOptions) error
				if values.Profile != "" {
					optFns = append(optFns, config.WithSharedConfigProfile(values.Profile))
				}
				optFns = append(optFns, config.WithRegion(rgn))

				cfg, err := config.LoadDefaultConfig(context.Background(), optFns...)
				if err != nil {
					results[idx] = regionResult{region: rgn, err: err}
					return
				}

				instanceTypesURL := values.InstanceTypesURL
				analyzer := rds.NewRDSRightSize(
					&instanceTypesURL,
					&cfg,
					values.Period,
					tags,
					values.CPUDownsize,
					values.CPUUpsize,
					values.MemUpsize,
					cwTypes.StatName(values.Stat),
					values.PreferNewGen,
					rgn,
				)

				opts := &rds.AnalysisOptions{
					FetchTimeSeries: true,
					OnProgress: func(current int, total int, instanceId string) {
						mu.Lock()
						rp := regionProg[rgn]
						rp.current = current
						rp.total = total
						var totalSum, currentSum int
						for _, p := range regionProg {
							totalSum += p.total
							currentSum += p.current
						}
						mu.Unlock()
						progressChan <- ProgressMsg{
							Current:    currentSum,
							Total:      totalSum,
							InstanceID: fmt.Sprintf("%s (%s)", instanceId, rgn),
						}
					},
					OnWarning: func(instanceId, msg string) {
						mu.Lock()
						allWarnings = append(allWarnings, fmt.Sprintf("%s (%s): %s", instanceId, rgn, msg))
						mu.Unlock()
					},
				}

				recommendations, err := analyzer.AnalyzeRDS(opts)
				if err != nil {
					results[idx] = regionResult{region: rgn, err: err}
					return
				}

				// Stamp region on each recommendation
				for j := range recommendations {
					recommendations[j].Region = rgn
				}
				results[idx] = regionResult{region: rgn, recommendations: recommendations}
			}(i, rgn)
		}

		wg.Wait()
		close(progressChan)

		// Merge results, collect errors
		allRecs := make([]types.Recommendation, 0)
		var errs []string
		for _, r := range results {
			if r.err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", r.region, r.err))
				continue
			}
			allRecs = append(allRecs, r.recommendations...)
		}

		if len(allRecs) == 0 && len(errs) > 0 {
			return AnalysisDoneMsg{Err: fmt.Errorf("all regions failed:\n%s", strings.Join(errs, "\n"))}
		}

		// Sort: cluster members grouped, then by instance ID
		sort.SliceStable(allRecs, func(i, j int) bool {
			ci := allRecs[i].DBClusterIdentifier
			cj := allRecs[j].DBClusterIdentifier
			if ci != nil && cj == nil {
				return true
			}
			if ci == nil && cj != nil {
				return false
			}
			if ci != nil && cj != nil && *ci != *cj {
				return *ci < *cj
			}
			ii := allRecs[i].DBInstanceIdentifier
			ij := allRecs[j].DBInstanceIdentifier
			if ii != nil && ij != nil {
				return *ii < *ij
			}
			return false
		})

		return AnalysisDoneMsg{Recommendations: allRecs, Warnings: allWarnings}
	}
}

func waitForProgress(ch chan ProgressMsg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}

func parseTags(tags string) map[string]string {
	tagsEntries := strings.Split(tags, ",")
	tagsMap := make(map[string]string)

	if len(tagsEntries) > 0 {
		for _, e := range tagsEntries {
			if len(strings.TrimSpace(e)) > 0 {
				parts := strings.Split(e, "=")
				if len(parts) == 2 {
					tagsMap[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
				}
			}
		}
	}

	return tagsMap
}

// splitRegions splits a comma-separated region string into a slice,
// trimming whitespace and filtering empty entries.
func splitRegions(s string) []string {
	var regions []string
	for _, r := range strings.Split(s, ",") {
		r = strings.TrimSpace(r)
		if r != "" {
			regions = append(regions, r)
		}
	}
	return regions
}

// Run starts the TUI application.
func Run(defaults ConfigValues) error {
	p := tea.NewProgram(NewModel(defaults), tea.WithAltScreen())
	_, err := p.Run()
	return err
}
