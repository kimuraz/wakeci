import Vue from "vue";
import Vuex from "vuex";
import state from "./state";
import actions from "./actions";
import getters from "./getters";
import mutations from "./mutations";

Vue.use(Vuex);

const debug = import.meta.env.NODE_ENV !== "production";

export default new Vuex.Store({
    state: state,
    getters: getters,
    actions: actions,
    mutations: mutations,
    strict: debug,
});
