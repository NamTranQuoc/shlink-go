package cli

import (
	"github.com/NamTranQuoc/shlink-go/internal/console"
	"github.com/NamTranQuoc/shlink-go/internal/core"
)

func init() {
	register("tag:list", Body{Execute: listTags})
	register("tag:rename", Body{Execute: renameTag})
	register("tag:delete", Body{Execute: deleteTags})
	register("tag:visits", Body{
		Interact: func(_ *Env, io *console.IO) error {
			return askArgument(io, "tag", "For what tag do you want to get visits")
		},
		Execute: tagVisits,
	})
}

func listTags(env *Env, io *console.IO) (int, error) {
	style := io.Style()
	s, err := env.Services()
	if err != nil {
		return 0, err
	}
	paginator, err := s.Tags.TagsInfo(env.Ctx, core.TagsParams{OrderBy: core.OrderingFromOptionalString(nil), Page: 1, ItemsPerPage: core.AllItems}, nil)
	if err != nil {
		return 0, err
	}
	tags, err := paginator.Results(env.Ctx)
	if err != nil {
		return 0, err
	}
	rows := [][]any{{"No tags found", "-", "-"}}
	if len(tags) > 0 {
		rows = rows[:0]
		for _, t := range tags {
			rows = append(rows, []any{t.Tag, t.ShortURLsCount, t.VisitsSummary.Total})
		}
	}
	shlinkTable(style, false, []any{"Name", "URLs amount", "Visits amount"}, rows, "", "")
	return console.Success, nil
}

func renameTag(env *Env, io *console.IO) (int, error) {
	style := io.Style()
	s, err := env.Services()
	if err != nil {
		return 0, err
	}
	r := core.Renaming{OldName: deref(io.Input.ArgumentString("old-name"), ""), NewName: deref(io.Input.ArgumentString("new-name"), "")}
	if err := s.Tags.RenameTag(env.Ctx, r, nil); err != nil {
		if core.IsKind(err, core.KindTagNotFound) || core.IsKind(err, core.KindTagConflict) {
			style.Error(err.Error())
			return console.Failure, nil
		}
		return 0, err
	}
	style.Success("Tag properly renamed.")
	return console.Success, nil
}

func deleteTags(env *Env, io *console.IO) (int, error) {
	style := io.Style()
	names := io.Input.OptionStrings("name")
	if len(names) == 0 {
		style.Warning("You have to provide at least one tag name")
		return console.Invalid, nil
	}
	s, err := env.Services()
	if err != nil {
		return 0, err
	}
	if err := s.Tags.DeleteTags(env.Ctx, names, nil); err != nil {
		return 0, err
	}
	style.Success("Tags properly deleted")
	return console.Success, nil
}

func tagVisits(env *Env, io *console.IO) (int, error) {
	style := io.Style()
	input, err := visitsListInput(io.Input)
	if err != nil {
		return 0, err
	}
	s, err := env.Services()
	if err != nil {
		return 0, err
	}
	params, err := input.params()
	if err != nil {
		return 0, err
	}
	params.Domain = io.Input.OptionString("domain")
	paginator, err := s.Visits.VisitsForTag(env.Ctx, deref(io.Input.ArgumentString("tag"), ""), params, nil)
	if err != nil {
		return 0, err
	}
	return console.Success, renderVisits(env, style, input, paginator)
}
