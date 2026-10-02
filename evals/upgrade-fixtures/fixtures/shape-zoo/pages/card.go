package pages

var markup = []string{
	`<DIV CLASS = ui-button>Go</DIV>`, // zoo:hit button-class
	`<p class="ui&#45;button">Go</p>`, // zoo:hit button-class
	`<a class="link" class="ui-button">Go</a>`,
	`<script type="text/plain"><b class="ui-button">Go</b></script>`,
	`<!-- a > b <b class="ui-button"> -->`,
	"ui-button-group",
}
