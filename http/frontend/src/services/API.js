import axios from "axios";

const instance = axios.create({
  baseURL: `http://192.168.1.90:3005/api/v1`,
  withCredentials: true,
});

instance.interceptors.response.use(
  (response) => {
    return response;
  },
  (error) => {
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
