export default {
  extends: ["@commitlint/config-conventional"],
  rules: {
    "header-max-length": [2, "always", 100],
    "trailer-exists": [2, "always", "Signed-off-by:"],
  },
};
