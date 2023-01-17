import axios from "axios";

const instance = axios.create({
  baseURL: `/api/v1`,
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
