package codegraph

import "slices"

// Issue is an unresolved relation, not a graph edge. Targets, when nonempty,
// exhaustively bound possible local targets; nil means the target is unknown.
type Issue struct {
	Path       string     `json:"path"`
	From       string     `json:"from,omitempty"`
	Kind       Kind       `json:"relation,omitempty"`
	Code       string     `json:"reason,omitempty"`
	Message    string     `json:"message"`
	Line       int        `json:"line,omitempty"`
	Targets    []string   `json:"possibleTargets,omitempty"`
	Confidence Confidence `json:"confidence,omitempty"`
}

type QueryIssue struct {
	Issue
	Candidates  []string
	Disposition string
}

type QueryResult struct {
	Paths                  map[string]Path
	Blocking, Observations []QueryIssue
}

// Query separates proven paths from possible paths through unresolved relations.
// +spec=`Missing edges never prove independence; unconstrained targets remain unknown`
// +why=`Completeness belongs to the requested relation query, not the whole graph`
func (g *Graph) Query(seeds, candidates []string, kinds []Kind, issues []Issue) QueryResult {
	result := QueryResult{Paths: g.Reverse(seeds, kinds)}
	// Inferred edges are retained for graph consumers, but never become proven
	// paths. Their explicit targets can only affect completeness here.
	issues = slices.Clone(issues)
	var nodes []string
	for id := range g.Nodes {
		nodes = append(nodes, id)
	}
	for _, id := range unique(nodes) {
		for _, edge := range g.Outgoing(id) {
			if edge.Confidence == "" {
				continue
			}
			issues = append(issues, Issue{Path: edge.File, From: edge.From, Kind: edge.Kind, Line: edge.Line,
				Targets: []string{edge.To}, Confidence: edge.Confidence, Code: "inferred_relation", Message: "relation target is inferred"})
		}
	}
	allowed := map[Kind]bool{}
	for _, kind := range kinds {
		allowed[kind] = true
	}
	diagnosticKinds := append(slices.Clone(kinds), ConfigScope)
	// Each possible node carries the issues on a hypothetical route. None of
	// these routes is inserted into Graph or returned as an evidence Path.
	possible := map[string]map[int]bool{}
	for node := range result.Paths {
		possible[node] = map[int]bool{}
		if n, ok := g.Nodes[node]; ok && n.File != "" {
			possible[n.File] = map[int]bool{}
		}
	}
	for _, seed := range seeds {
		possible[seed] = map[int]bool{}
		if n, ok := g.Nodes[seed]; ok && n.File != "" {
			possible[n.File] = map[int]bool{}
		}
	}
	ancestors := map[string][]string{}
	ancestorsOf := func(from string) []string {
		if found, ok := ancestors[from]; ok {
			return found
		}
		starts := []string{from}
		// Ownership may widen uncertainty, never evidence. A file importer can
		// depend on an unresolved symbol in that file without naming it.
		if node, ok := g.Nodes[from]; ok && node.File != "" {
			starts = append(starts, node.File)
		}
		found := slices.Clone(starts)
		for node := range g.Reverse(starts, diagnosticKinds) {
			found = append(found, node)
		}
		found = unique(found)
		ancestors[from] = found
		return found
	}
	eligible := func(issue Issue) bool { return issue.Kind == "" || allowed[issue.Kind] }
	for changed := true; changed; {
		changed = false
		for i, issue := range issues {
			if !eligible(issue) {
				continue
			}
			causes := map[int]bool{i: true}
			reachable := len(issue.Targets) == 0
			for _, target := range issue.Targets {
				if upstream, ok := possible[target]; ok {
					reachable = true
					for cause := range upstream {
						causes[cause] = true
					}
				}
			}
			if !reachable {
				continue
			}
			from := issue.From
			if from == "" {
				from = issue.Path
			}
			for _, node := range ancestorsOf(from) {
				if possible[node] == nil {
					possible[node] = map[int]bool{}
				}
				for cause := range causes {
					if !possible[node][cause] {
						possible[node][cause] = true
						changed = true
					}
				}
			}
		}
	}
	affected := map[int][]string{}
	for _, candidate := range unique(candidates) {
		if _, proven := result.Paths[candidate]; proven {
			continue
		}
		for cause := range possible[candidate] {
			affected[cause] = append(affected[cause], candidate)
		}
	}
	for i, issue := range issues {
		q := QueryIssue{Issue: issue, Candidates: affected[i]}
		if len(q.Candidates) > 0 {
			q.Disposition = "may_change_result"
			result.Blocking = append(result.Blocking, q)
		} else {
			q.Disposition = "outside_query"
			if !eligible(issue) {
				q.Disposition = "relation_not_requested"
			}
			result.Observations = append(result.Observations, q)
		}
	}
	return result
}
