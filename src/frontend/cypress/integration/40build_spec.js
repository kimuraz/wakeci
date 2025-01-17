describe("Build page", function() {
    it("should show build page", function() {
        // Create job
        const jobName = "myjob" + new Date().getTime();
        cy.request({
            url: "/api/jobs/create",
            method: "POST",
            auth: {
                user: "",
                pass: "admin",
            },
            body: {
                "name": jobName,
            },
            form: true,
        });
        // Create build
        cy.request({
            url: `/api/job/${jobName}/run`,
            method: "POST",
            auth: {
                user: "",
                pass: "admin",
            },
            body: {},
            form: true,
        });
        cy.visit("/");
        cy.login();
        cy.get("[data-cy=filter]").clear().type(jobName);
        cy.get("tr").invoke("attr", "data-cy-build").then((val) => {
            cy.get("[data-cy=open-build-button]").click();
            cy.url().should("include", "/build/" + val);
            cy.get("[data-cy=reload]").click();
            cy.get(".notification-content").should("contain", "Log file has been reloaded");
            cy.get("body").should("contain", "uname -a");
        });
    });

    it("should inject wake env variables", function() {
        // Create job
        const jobName = "myjob" + new Date().getTime();
        cy.request({
            url: "/api/jobs/create",
            method: "POST",
            auth: {
                user: "",
                pass: "admin",
            },
            body: {
                "name": jobName,
            },
            form: true,
        });

        const jobContent = `
desc: Test env variables
params:
  - pruzhany: 5
  - minsk: 4
tasks:
  - name: Print env
    run: env
`;

        cy.request({
            url: "/api/job/" + jobName,
            method: "POST",
            auth: {
                user: "",
                pass: "admin",
            },
            body: {
                "fileContent": jobContent,
            },
            form: true,
        });

        // Create build
        cy.request({
            url: `/api/job/${jobName}/run`,
            method: "POST",
            auth: {
                user: "",
                pass: "admin",
            },
            body: {},
            form: true,
        });

        cy.visit("/");
        cy.login();
        cy.get("[data-cy=filter]").clear().type(jobName);
        cy.get("tr").invoke("attr", "data-cy-build").then((val) => {
            cy.get("[data-cy=open-build-button]").click();
            cy.url().should("include", "/build/" + val);
            cy.get("[data-cy=reload]").click();
            cy.get(".notification-content").should("contain", "Log file has been reloaded");
            cy.get("body").should("contain", `WAKE_JOB_NAME=${jobName}`);
            cy.get("body").should("contain", "WAKE_URL=http://localhost:8081/");
            cy.get("body").should("contain", "WAKE_CONFIG_DIR=");
            cy.get("body").should("contain", "WAKE_BUILD_WORKSPACE=");
            cy.get("body").should("contain", "WAKE_BUILD_ID=");
            cy.get("body").should("contain", "WAKE_JOB_PARAMS=minsk=4&pruzhany=5");
        });
    });

    it("should collect artifacts", function() {
        // Create job
        const jobName = "myjob" + new Date().getTime();
        cy.request({
            url: "/api/jobs/create",
            method: "POST",
            auth: {
                user: "",
                pass: "admin",
            },
            body: {
                "name": jobName,
            },
            form: true,
        });

        const jobContent = `
desc: Test env variables
params:
  - pruzhany: 5
  - minsk: 4
tasks:
  - name: Create 1 file
    run: journalctl -n 100 > big

  - name: Create 2 file
    run: journalctl -n 50 > small

artifacts:
  - "*"
`;

        cy.request({
            url: "/api/job/" + jobName,
            method: "POST",
            auth: {
                user: "",
                pass: "admin",
            },
            body: {
                "fileContent": jobContent,
            },
            form: true,
        });

        // Create build
        cy.request({
            url: `/api/job/${jobName}/run`,
            method: "POST",
            auth: {
                user: "",
                pass: "admin",
            },
            body: {},
            form: true,
        });

        cy.visit("/");
        cy.login();
        cy.get("[data-cy=filter]").clear().type(jobName);
        cy.get("tr").invoke("attr", "data-cy-build").then((val) => {
            cy.get("[data-cy=open-build-button]").click();

            // Assert total number of artifacts
            cy.get("[data-cy=artifacts-body-row]").should("have.length", 2);

            // Assert order by name
            cy.get("[data-cy=artifacts-body-row] > td:first").should("contain", "big");
            cy.get("[data-cy=artifacts-header-file]").click();
            cy.get("[data-cy=artifacts-body-row] > td:first").should("contain", "small");
            cy.get("[data-cy=artifacts-header-file]").click();
            cy.get("[data-cy=artifacts-body-row] > td:first").should("contain", "big");

            // Make sure Open index.html button is not present
            cy.get("[data-cy=openIndexFile]").should("not.exist");
        });
    });

    it("should display Open index.html button", function() {
        // Create job
        const jobName = "myjob" + new Date().getTime();
        cy.request({
            url: "/api/jobs/create",
            method: "POST",
            auth: {
                user: "",
                pass: "admin",
            },
            body: {
                "name": jobName,
            },
            form: true,
        });

        const jobContent = `
desc: Test env variables
params:
  - pruzhany: 5
  - minsk: 4
tasks:
  - name: Create 1 file
    run: mkdir -p bb && journalctl -n 100 > bb/index.html

  - name: Create 2 file
    run: mkdir -p aa/cc && journalctl -n 50 > aa/cc/index.html

artifacts:
  - "**"
`;

        cy.request({
            url: "/api/job/" + jobName,
            method: "POST",
            auth: {
                user: "",
                pass: "admin",
            },
            body: {
                "fileContent": jobContent,
            },
            form: true,
        });

        // Create build
        cy.request({
            url: `/api/job/${jobName}/run`,
            method: "POST",
            auth: {
                user: "",
                pass: "admin",
            },
            body: {},
            form: true,
        });

        cy.visit("/");
        cy.login();
        cy.get("[data-cy=filter]").clear().type(jobName);
        cy.get("tr").invoke("attr", "data-cy-build").then((val) => {
            cy.get("[data-cy=open-build-button]").click();

            // Make sure Open index.html button is present
            cy.get("[data-cy=openIndexFile]").should("contain", "Open index.html");
            cy.get("[data-cy=openIndexFile]").should("have.attr", "href").and("contain", "bb");
        });
    });

    it("should skip task when condition is false", function() {
        // Create job
        const jobName = "myjob" + new Date().getTime();
        cy.request({
            url: "/api/jobs/create",
            method: "POST",
            auth: {
                user: "",
                pass: "admin",
            },
            body: {
                "name": jobName,
            },
            form: true,
        });

        const jobContent = `
desc: Condition test
tasks:
  - name: Print env
    run: env
    when: 1 == 2
`;

        cy.request({
            url: "/api/job/" + jobName,
            method: "POST",
            auth: {
                user: "",
                pass: "admin",
            },
            body: {
                "fileContent": jobContent,
            },
            form: true,
        });

        // Create build
        cy.request({
            url: `/api/job/${jobName}/run`,
            method: "POST",
            auth: {
                user: "",
                pass: "admin",
            },
            body: {},
            form: true,
        });

        cy.visit("/");
        cy.login();
        cy.get("[data-cy=filter]").clear().type(jobName);
        cy.get("tr").invoke("attr", "data-cy-build").then((val) => {
            cy.get("[data-cy=open-build-button]").click();
            cy.url().should("include", "/build/" + val);
            cy.get("[data-cy=reload]").click();
            cy.get(".notification-content").should("contain", "Log file has been reloaded");
            cy.get("body").should("contain", "Condition is false");
            cy.get("body").should("not.contain", "WAKE_BUILD_ID=");
        });
    });

    it("should run task when condition is true", function() {
        // Create job
        const jobName = "myjob" + new Date().getTime();
        cy.request({
            url: "/api/jobs/create",
            method: "POST",
            auth: {
                user: "",
                pass: "admin",
            },
            body: {
                "name": jobName,
            },
            form: true,
        });

        const jobContent = `
desc: Condition test
params:
 - NAME: joe
tasks:
  - name: Print env
    run: env
    when: $NAME == joe
`;

        cy.request({
            url: "/api/job/" + jobName,
            method: "POST",
            auth: {
                user: "",
                pass: "admin",
            },
            body: {
                "fileContent": jobContent,
            },
            form: true,
        });

        // Create build
        cy.request({
            url: `/api/job/${jobName}/run`,
            method: "POST",
            auth: {
                user: "",
                pass: "admin",
            },
            body: {},
            form: true,
        });

        cy.visit("/");
        cy.login();
        cy.get("[data-cy=filter]").clear().type(jobName);
        cy.get("tr").invoke("attr", "data-cy-build").then((val) => {
            cy.get("[data-cy=open-build-button]").click();
            cy.url().should("include", "/build/" + val);
            cy.get("[data-cy=reload]").click();
            cy.get(".notification-content").should("contain", "Log file has been reloaded");
            cy.get("body").should("contain", "Condition is true");
            cy.get("body").should("contain", "WAKE_BUILD_ID=");
        });
    });

    it("should inject env variables", function() {
        // Create job
        const jobName = "myjob" + new Date().getTime();
        cy.request({
            url: "/api/jobs/create",
            method: "POST",
            auth: {
                user: "",
                pass: "admin",
            },
            body: {
                "name": jobName,
            },
            form: true,
        });

        const jobContent = `
desc: Env test
tasks:
  - name: Print env
    run: env
    env:
      NAME: joe
      SCORE: 5
`;

        cy.request({
            url: "/api/job/" + jobName,
            method: "POST",
            auth: {
                user: "",
                pass: "admin",
            },
            body: {
                "fileContent": jobContent,
            },
            form: true,
        });

        // Create build
        cy.request({
            url: `/api/job/${jobName}/run`,
            method: "POST",
            auth: {
                user: "",
                pass: "admin",
            },
            body: {},
            form: true,
        });

        cy.visit("/");
        cy.login();
        cy.get("[data-cy=filter]").clear().type(jobName);
        cy.get("tr").invoke("attr", "data-cy-build").then((val) => {
            cy.get("[data-cy=open-build-button]").click();
            cy.url().should("include", "/build/" + val);
            cy.get("[data-cy=reload]").click();
            cy.get("body").should("contain", "NAME=joe");
            cy.get("body").should("contain", "SCORE=5");
            cy.get("body").should("contain", "WAKE_BUILD_ID=");
        });
    });
});
