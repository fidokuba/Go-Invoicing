import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { client } from "../client";
import { unwrap } from "../unwrap";
import type { components } from "../schema";

type CreateProductRequest = components["schemas"]["CreateProductRequest"];

export interface ProductsListParams {
  limit?: number;
  offset?: number;
  search?: string;
  isActive?: boolean;
  sort?: "name" | "sku" | "price" | "createdAt";
  order?: "asc" | "desc";
}

export function useProducts(params: ProductsListParams) {
  return useQuery({
    queryKey: ["products", params],
    queryFn: () => unwrap(client.GET("/api/v1/products", { params: { query: params } })),
  });
}

export function useProduct(id: string | undefined) {
  return useQuery({
    queryKey: ["products", id],
    queryFn: () => unwrap(client.GET("/api/v1/products/{id}", { params: { path: { id: id! } } })),
    enabled: Boolean(id),
  });
}

export function useCreateProduct() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (body: CreateProductRequest) => unwrap(client.POST("/api/v1/products", { body })),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["products"] });
    },
  });
}
