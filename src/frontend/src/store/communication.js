const wsMessageHandler = function(app, data) {
    const messages = data.split("\n");
    for (let i = 0; i < messages.length; i++) {
        const msg = JSON.parse(messages[i]);
        if (msg.type.startsWith("build:log:")) {
            app.$eventHub.$emit(msg.type, msg.data);
            continue;
        } else if (msg.type.startsWith("build:update:")) {
            // For build view
            app.$eventHub.$emit(msg.type, msg.data);
            // For feed view
            app.$eventHub.$emit("build:update:", msg.data);
            continue;
        }
        console.warn("Unhandled message", msg);
    }
};

export const getWSURL = function() {
    let protocol; let hostname;
    if (location.protocol === "https:") {
        protocol = "wss://";
    } else {
        protocol = "ws://";
    }

    if (import.meta.env.NODE_ENV === "production") {
        hostname = location.host;
    } else {
        hostname = "localhost:8081";
    }
    return `${protocol}${hostname}/ws`;
};

export default wsMessageHandler;
