package pages

var markup = []string{
	`<DIV CLASS = fui-button>Go</DIV>`,
	`<p class="fui&#45;button">Go</p>`,
	`<a class="link" class="ui-button">Go</a>`,
	`<script type="text/plain"><b class="ui-button">Go</b></script>`,
	`<!-- a > b <b class="ui-button"> -->`,
	"ui-button-group",
}
