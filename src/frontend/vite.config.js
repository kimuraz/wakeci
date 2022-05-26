import path from "path";
import {createVuePlugin} from "vite-plugin-vue2";

export default {
    plugins: [createVuePlugin()],
    resolve: {
        alias: {
            "@": path.resolve(__dirname, "./src"),
        },
        extensions: [".js", ".vue", ".json", ".yml"],
    },
    server: {
        proxy: {
            "^/api": {
                target: "http://localhost:8081/",
                changeOrigin: true,
            },
            "^/auth": {
                target: "http://localhost:8081/",
                changeOrigin: true,
            },
            "^/storage": {
                target: "http://localhost:8081/",
                changeOrigin: true,
            },
        },
    },
};
