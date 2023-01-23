import axios from "axios";

var baseURL;

// nodejs development
if (window.location.port == 5173) {
  // Change port to 3005
  baseURL = "http://127.0.0.1:3005/api/v1";
} else {
  baseURL = "/api/v1";
}

const instance = axios.create({
  baseURL,
  withCredentials: true,
});

instance.interceptors.response.use(
  (response) => {
    return response;
  },
  (error) => {
    if (error.response === undefined) {
      return Promise.reject(error);
    }
    const status = error.response.status;
    if (
      window.location.pathname !== "/login" &&
      window.location.pathname !== "/" &&
      window.location.pathname !== "/register" &&
      (status === 401 || status === 403)
    ) {
      window.location = "/login";
    }

    return Promise.reject(error);
  }
);

export default instance;
