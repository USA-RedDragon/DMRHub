import { toRefs, reactive } from "vue";

const layoutConfig = reactive({
  theme: "mdc-dark-indigo",
  scale: 16,
});

export function useLayout() {
  const changeThemeSettings = (theme) => {
    layoutConfig.theme = theme;
  };

  const setScale = (scale) => {
    layoutConfig.scale = scale;
    window.localStorage.setItem("scale", scale);
  };

  return {
    layoutConfig: toRefs(layoutConfig),
    changeThemeSettings,
    setScale,
  };
}
