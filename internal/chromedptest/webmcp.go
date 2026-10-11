package chromedptest

import "fmt"

// WebMCPExecuteExpr builds an async browser expression that looks up a
// registered tool by name and executes it with a trusted input object literal.
// It accepts browsers requiring an object or a JSON string: only the native
// input-parsing refusal retries as a string, never an execution error.
// The browser returns the tool result as JSON text.
func WebMCPExecuteExpr(name, inputLiteral string) string {
	return fmt.Sprintf(`(async () => {
		const mc = document.modelContext || navigator.modelContext;
		const tools = await mc.getTools();
		const tool = tools.find(t => t.name === %q);
		if (!tool) return JSON.stringify({error: "not registered"});
		const input = %s;
		try {
			return await mc.executeTool(tool, input);
		} catch (err) {
			if (err && err.name === 'UnknownError' && /parse input/.test(String(err.message))) {
				return mc.executeTool(tool, JSON.stringify(input));
			}
			throw err;
		}
	})()`, name, inputLiteral)
}
