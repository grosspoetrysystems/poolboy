import {
  createEngine,
  type FilterRegistry,
  standardFilters,
  type TemplateResult,
  type TemplateVariables,
} from "knap";

const ALLOWED_FILTERS = [
  "trim",
  "upper",
  "lower",
  "first",
  "last",
  "length",
  "join",
  "list",
  "table",
  "yaml",
  "yaml_property",
  "h1",
  "h2",
  "code",
  "code_block",
  "indent",
  "escape_md",
  "link",
] as const;

export const RENDER_LIMITS = {
  maxDepth: 50,
  maxOperations: 50_000,
  maxOutputLength: 100_000,
  maxTemplateLength: 100_000,
  maxValueLength: 1_000_000,
} as const;

function createFilterRegistry(): FilterRegistry {
  const filters: FilterRegistry = {};
  for (const name of ALLOWED_FILTERS) {
    const filter = standardFilters[name];
    if (typeof filter !== "function") {
      throw new Error(`Knap filter is unavailable: ${name}`);
    }
    filters[name] = filter;
  }
  return filters;
}

const engine = createEngine({
  allowRegex: false,
  filters: createFilterRegistry(),
  limits: RENDER_LIMITS,
});

export function render(
  template: string,
  variables: TemplateVariables
): Promise<TemplateResult> {
  return engine.render(template, { variables }, { trimOutput: false });
}
