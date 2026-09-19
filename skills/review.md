# Review Skill

When reviewing code:
- Check for correctness first, maintainability second.
- Verify error wrapping uses `%w` for causal chains.
- Ensure `context.Context` is passed to all blocking methods.
