package review

import (
	"context"
	"fmt"
	"github.com/lemon4ksan/g-man/pkg/log"
	"strings"
	"time"
)

type Reviewer struct {
	schema SchemaProvider
	chat   ChatProvider
	logger log.Logger
}

func New(s SchemaProvider, c ChatProvider, l log.Logger) *Reviewer {
	return &Reviewer{schema: s, chat: c, logger: l}
}

// BuildSummary generates a structured report based on metadata.
func (rv *Reviewer) BuildSummary(meta *TradeMetadata, f Formatter) *Report {
	report := &Report{}

	if reg, ok := reasonRegistry[meta.PrimaryReason]; ok {
		report.MainReason = reg.Description
	}

	for _, r := range meta.Reasons {
		rt := r.ReasonType()
		if reg, ok := reasonRegistry[rt]; ok && reg.Processor != nil {
			line := reg.Processor(r, rv.schema, f)
			report.Details = append(report.Details, fmt.Sprintf("[%s] %s", rt, line))
		}
	}

	return report
}

// SendDeclinedAlert sends a detailed alert to the admin about why the offer was rejected.
func (rv *Reviewer) SendDeclinedAlert(ctx context.Context, offerID uint64, partnerID uint64, meta *TradeMetadata, stats BotStatsProvider) error {
	f := SteamFormatter{}
	report := rv.BuildSummary(meta, f)

	var sb strings.Builder
	fmt.Fprintf(&sb, f.Header("Trade #%d with %d declined. ❌\n"), offerID, partnerID)
	fmt.Fprintf(&sb, "Reason: %s\n", report.MainReason)

	if len(report.Details) > 0 {
		sb.WriteString("\nDetails:\n- " + strings.Join(report.Details, "\n- "))
	}

	keys, ref := stats.GetPureStock()
	fmt.Fprintf(&sb, "\n\n💰 Stock: %.2f keys, %.2f ref", keys, ref)
	fmt.Fprintf(&sb, "\n🎒 Backpack: %d/%d", stats.GetTotalItems(), stats.GetBackpackSlots())

	duration := time.Duration(meta.ProcessTimeMS) * time.Millisecond
	fmt.Fprintf(&sb, "\n⏱ Processed in: %s", duration.String())

	return rv.chat.MessageAdmins(ctx, sb.String())
}

// SendReviewAlert sends a detailed message to the administrator that the offer
// is awaiting manual approval and provides instructions on how to proceed.
func (rv *Reviewer) SendReviewAlert(ctx context.Context, offerID uint64, partnerID uint64, meta *TradeMetadata) error {
	f := SteamFormatter{}
	report := rv.BuildSummary(meta, f)

	var sb strings.Builder

	fmt.Fprint(&sb, f.Header("Manual Review Required! ⚠️\n"))
	fmt.Fprintf(&sb, "Offer #%d from user %d is pending your decision.\n", offerID, partnerID)

	if report.MainReason != "" {
		fmt.Fprintf(&sb, "\nMain Reason: %s\n", report.MainReason)
	}

	if len(report.Details) > 0 {
		sb.WriteString("\nDetected Issues:\n- " + strings.Join(report.Details, "\n- "))
	}

	fmt.Fprintf(&sb, "\n\n📋 Commands to respond:")
	fmt.Fprintf(&sb, "\n- !accept %d", offerID)
	fmt.Fprintf(&sb, "\n- !decline %d", offerID)

	if meta.ProcessTimeMS > 0 {
		duration := time.Duration(meta.ProcessTimeMS) * time.Millisecond
		fmt.Fprintf(&sb, "\n\n⏱ Engine processing time: %s", duration.String())
	}

	return rv.chat.MessageAdmins(ctx, sb.String())
}

// Report is an intermediate object with strings ready for output.
type Report struct {
	MainReason string
	Details    []string
}
